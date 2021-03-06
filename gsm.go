package gogsmmodem

import (
	"bufio"
	"errors"
	"io"
	"log"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tarm/serial"
)

type encodeMode uint8

const (
	GSM = encodeMode(iota)
	UCS2
)

var EncodeMode encodeMode
var SMSCGsm interface{}
var SMSCUcs2 interface{}

type Modem struct {
	OOB   chan Packet
	Debug bool
	port  io.ReadWriteCloser
	rx    chan Packet
	tx    chan string
}

var OpenPort = func(config *serial.Config) (io.ReadWriteCloser, error) {
	return serial.OpenPort(config)
}

func Open(config *serial.Config, debug bool) (*Modem, error) {
	port, err := OpenPort(config)
	if debug {
		port = LogReadWriteCloser{port}
	}
	if err != nil {
		return nil, err
	}
	oob := make(chan Packet, 16)
	rx := make(chan Packet)
	tx := make(chan string)
	modem := &Modem{
		OOB:   oob,
		Debug: debug,
		port:  port,
		rx:    rx,
		tx:    tx,
	}
	// run send/receive goroutine
	go modem.listen()

	err = modem.init()
	if err != nil {
		return nil, err
	}
	return modem, nil
}

func (self *Modem) Close() error {
	close(self.OOB)
	close(self.rx)
	// close(self.tx)
	return self.port.Close()
}

// Commands

// GetMessage by index n from memory.
func (self *Modem) GetMessage(n int) (*Message, error) {
	packet, err := self.send("+CMGR", n)
	if err != nil {
		return nil, err
	}
	if msg, ok := packet.(Message); ok {
		return &msg, nil
	}
	return nil, errors.New("Message not found")
}

// GetMessagePDU by index n from memory in pdu format.
func (self *Modem) GetMessagePDU(n int) (*Message, error) {
	time.Sleep(1 * time.Second)
	self.send("+CMGF", 0)
	time.Sleep(1 * time.Second)
	packet, err := self.send("+CMGR", n)
	if err != nil {
		return nil, err
	}
	time.Sleep(1 * time.Second)
	self.send("+CMGF", 1)
	if msg, ok := packet.(Message); ok {
		return &msg, nil
	}
	return nil, errors.New("Message not found")
}

// ListMessages stored in memory. Filter should be "ALL", "REC UNREAD", "REC READ", etc.
func (self *Modem) ListMessages(filter string) (*MessageList, error) {
	packet, err := self.send("+CMGL", filter)
	if err != nil {
		return nil, err
	}
	res := MessageList{}
	if _, ok := packet.(OK); ok {
		// empty response
		return &res, nil
	}

	for {
		if msg, ok := packet.(Message); ok {
			res = append(res, msg)
			if msg.Last {
				break
			}
		} else {
			return nil, errors.New("Unexpected error")
		}

		packet = <-self.rx
	}
	return &res, nil
}

func (self *Modem) SupportedStorageAreas() (*StorageAreas, error) {
	packet, err := self.send("+CPMS", "?")
	if err != nil {
		return nil, err
	}
	if msg, ok := packet.(StorageAreas); ok {
		return &msg, nil
	}
	return nil, errors.New("Unexpected response type")
}

func (self *Modem) DeleteMessage(n int) error {
	_, err := self.send("+CMGD", n)
	return err
}

func (self *Modem) SendMessage(telephone, body string) error {
	var enc string
	if EncodeMode == UCS2 {
		enc = unicodeEncode(body)
		telephone = unicodeEncode(telephone)
	} else {
		enc = body
	}
	_, err := self.sendBody("+CMGS", enc, telephone)
	return err
}

func (self *Modem) SendMessagePDU(length int, body string) error {
	time.Sleep(1 * time.Second)
	self.send("+CMGF", 0)
	time.Sleep(1 * time.Second)
	_, err := self.sendBody("+CMGS", body, length)
	time.Sleep(1 * time.Second)
	self.send("+CMGF", 1)
	return err
}

func lineChannel(r io.Reader) chan string {
	ret := make(chan string)
	go func() {
		buffer := bufio.NewReader(r)
		for {
			line, _ := buffer.ReadString(10)
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				continue
			}
			log.Println("ret <- line")
			ret <- line
		}
	}()
	return ret
}

var reQuestion = regexp.MustCompile(`AT(\+[A-Z]+)`)

func isFinalStatus(status string) bool {
	return status == "OK" ||
		status == "ERROR" ||
		strings.Contains(status, "+CMS ERROR") ||
		strings.Contains(status, "+CME ERROR")
}

func parsePacket(status, header, body string) Packet {
	if header == "" && isFinalStatus(status) {
		if status == "OK" {
			return OK{}
		} else {
			return ERROR{}
		}
	}

	ls := strings.SplitN(header, ":", 2)
	if len(ls) != 2 {
		return UnknownPacket{header, []interface{}{}}
	}
	uargs := strings.TrimSpace(ls[1])
	args := unquotes(uargs)
	switch ls[0] {
	case "+ZUSIMR":
		// message storage unset nag, ignore
		return nil
	case "+ZPASR":
		return ServiceStatus{args[0].(string)}
	case "+ZDONR":
		return NetworkStatus{args[0].(string)}
	case "+CMTI":
		return MessageNotification{args[0].(string), args[1].(int)}
	case "+CSCA":
		return SMSCAddress{args}
	case "+CMGR":
		//if CMGF=0 then we just need the body in pdu format
		if args[1] == "" {
			return Message{Body: body}
		} else {
			return Message{Status: args[0].(string), Telephone: args[1].(string),
				Timestamp: parseTime(args[3].(string)), Body: body}
		}
	case "+CMGL":
		if reflect.TypeOf(args[2]).String() == "int" {
			return Message{
				Index:     args[0].(int),
				Status:    args[1].(string),
				Telephone: strconv.Itoa(args[2].(int)),
				Body:      body,
				Last:      status != "",
			}
		} else {
			return Message{
				Index:     args[0].(int),
				Status:    args[1].(string),
				Telephone: args[2].(string),
				Timestamp: parseTime(args[4].(string)),
				Body:      body,
				Last:      status != "",
			}
		}

	case "+CPMS":
		s := uargs
		if strings.HasPrefix(s, "(") {
			// query response
			// ("A","B","C"),("A","B","C"),("A","B","C")
			s = strings.TrimPrefix(s, "(")
			s = strings.TrimSuffix(s, ")")
			areas := strings.SplitN(s, "),(", 3)
			return StorageAreas{
				stringsUnquotes(areas[0]),
				stringsUnquotes(areas[1]),
				stringsUnquotes(areas[2]),
			}
		} else {
			// set response
			// 0,100,0,100,0,100
			// get ints
			var iargs []int
			for _, arg := range args {
				if iarg, ok := arg.(int); ok {
					iargs = append(iargs, iarg)
				}
			}
			if len(iargs) == 6 {
				return StorageInfo{
					iargs[0], iargs[1], iargs[2], iargs[3], iargs[4], iargs[5],
				}
			} else if len(iargs) == 4 {
				return StorageInfo{
					iargs[0], iargs[1], iargs[2], iargs[3], 0, 0,
				}
			}
			break

		}
	case "":
		if status == "OK" {
			return OK{}
		} else {
			return ERROR{}
		}
	}
	return UnknownPacket{ls[0], args}
}

func (self *Modem) listen() {
	in := lineChannel(self.port)
	var echo, last, header, body string
	for {
		select {
		case line := <-in:
			log.Println("case line := <-in")
			if line == echo {
				continue // ignore echo of command
			} else if last != "" && startsWith(line, last) {
				if header != "" {
					// first of multiple responses (eg CMGL)
					packet := parsePacket("", header, body)
					self.rx <- packet
				}
				header = line
				body = ""
			} else if isFinalStatus(line) {
				packet := parsePacket(line, header, body)
				self.rx <- packet
				header = ""
				body = ""
			} else if header != "" {
				// the body following a header
				body += line
			} else if line == "> " {
				// raw mode for body
			} else {
				// OOB packet
				log.Println("OOB packet")
				log.Println("line", line)
				log.Println("header", header)
				p := parsePacket("OK", line, "")
				if p != nil {
					log.Println("self.OOB <- p", p)
					// self.OOB <- p
				}
			}
		case line := <-self.tx:
			log.Println("**listen**")
			m := reQuestion.FindStringSubmatch(line)
			if len(m) > 0 {
				last = m[1]
			}
			echo = strings.TrimRight(line, "\r\n")
			self.port.Write([]byte(line))
			// //channel for timeout process
			// c1 := make(chan string, 1)
			// go func() {
			// 	self.port.Write([]byte(line))
			// 	c1 <- ""
			// }()
			// select {
			// case <-c1:
			// case <-time.After(10 * time.Second):
			// }
		}
	}
}

func formatCommand(cmd string, args ...interface{}) string {
	line := "AT" + cmd
	if len(args) > 0 {
		line += "=" + quotes(args)
	}
	line += "\r\n"
	return line
}

func (self *Modem) sendBody(cmd string, body string, args ...interface{}) (Packet, error) {
	self.tx <- formatCommand(cmd, args...)
	time.Sleep(1 * time.Second)
	self.tx <- body + "\x1A"
	time.Sleep(1 * time.Second)
	response := <-self.rx
	if _, e := response.(ERROR); e {
		return response, errors.New("Response was ERROR")
	}
	return response, nil
}

func (self *Modem) send(cmd string, args ...interface{}) (Packet, error) {
	self.tx <- formatCommand(cmd, args...)
	response := <-self.rx
	if _, e := response.(ERROR); e {
		return response, errors.New("Response was ERROR")
	}
	return response, nil
}

func (self *Modem) init() error {
	self.send("")
	time.Sleep(1 * time.Second)
	// clear settings
	self.send("Z")
	log.Println("Reset")
	time.Sleep(1 * time.Second)

	if EncodeMode == UCS2 {
		err := self.setSMSC(GSM)
		if err != nil {
			return err
		}
		time.Sleep(1 * time.Second)
		err = self.ChangeToUCS2()
		if err != nil {
			return err
		}
		time.Sleep(1 * time.Second)
	} else {
		self.ChangeToUCS2()
		time.Sleep(1 * time.Second)
		self.ChangeToGSM()
		time.Sleep(1 * time.Second)
	}

	// set SMS text mode - easiest to implement. Ignore response which is
	// often a benign error.
	self.send("+CMGF", 1)
	log.Println("Set SMS text mode")
	time.Sleep(1 * time.Second)

	//set delivery
	self.send("+CNMI", 2, 2, 0, 1, 0)
	log.Println("Set SMS delivery")
	time.Sleep(1 * time.Second)

	return nil
}

func (self *Modem) setSMSC(encode encodeMode) error {
	r, err := self.send("+CSCA?")
	if err != nil {
		return err
	}
	smsc := r.(SMSCAddress)
	log.Println("Got SMSC: ", smsc.Args)
	time.Sleep(1 * time.Second)
	if encode == UCS2 {
		SMSCUcs2 = smsc.Args[0]
	} else {
		SMSCGsm = smsc.Args[0]
	}
	r, err = self.send("+CSCA", smsc.Args...)
	if err != nil {
		return err
	}
	log.Println("Set SMSC to:", smsc.Args)
	return nil
}

func (self *Modem) ChangeToUCS2() error {
	EncodeMode = UCS2
	if _, err := self.send("+CSCS", "UCS2"); err != nil {
		return err
	}
	log.Println("Set SMS character encoding")
	time.Sleep(1 * time.Second)

	if _, err := self.send("+CSMP", 49, 167, 0, 8); err != nil {
		return err
	}
	log.Println("Set data coding schema")
	time.Sleep(1 * time.Second)
	err := self.setSMSC(UCS2)
	if err != nil {
		return err
	}
	return nil
}

func (self *Modem) ChangeToGSM() error {
	EncodeMode = GSM
	if _, err := self.send("+CSCS", "GSM"); err != nil {
		return err
	}
	log.Println("Set SMS character encoding")
	time.Sleep(1 * time.Second)

	if _, err := self.send("+CSMP", 49, 167, 0, 0); err != nil {
		return err
	}
	log.Println("Set data coding schema")
	time.Sleep(1 * time.Second)
	err := self.setSMSC(GSM)
	if err != nil {
		return err
	}
	return nil
}
