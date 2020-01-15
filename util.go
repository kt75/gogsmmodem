package gogsmmodem

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"io"
)

// Time format in AT protocol
var TimeFormat = "06/01/02,15:04:05"

// Parse an AT formatted time
func parseTime(t string) time.Time {
	t = t[:len(t)-3] // ignore trailing +00
	ret, _ := time.Parse(TimeFormat, t)
	return ret
}

// Quote a value
func quote(s interface{}) string {
	switch v := s.(type) {
	case string:
		if v == "?" {
			return v
		}
		return fmt.Sprintf(`"%s"`, v)
	case int, int64:
		return fmt.Sprint(v)
	default:
		panic(fmt.Sprintf("Unsupported argument type: %T", v))
	}
	return ""
}

// Quote a list of values
func quotes(args []interface{}) string {
	ret := make([]string, len(args))
	for i, arg := range args {
		ret[i] = quote(arg)
	}
	return strings.Join(ret, ",")
}

// Check if s starts with p
func startsWith(s, p string) bool {
	return strings.Index(s, p) == 0
}

// Unquote a string to a value (string or int)
func unquote(s string) interface{} {
	if startsWith(s, `"`) {
		return strings.Trim(s, `"`)
	}
	if i, err := strconv.Atoi(s); err == nil {
		// number
		return i
	}
	return s
}

var RegexQuote = regexp.MustCompile(`"[^"]*"|[^,]*`)

// Unquote a parameter list to values
func unquotes(s string) []interface{} {
	vs := RegexQuote.FindAllString(s, -1)
	args := make([]interface{}, len(vs))
	for i, v := range vs {
		args[i] = unquote(v)
	}
	return args
}

// Unquote a parameter list of strings
func stringsUnquotes(s string) []string {
	args := unquotes(s)
	var res []string
	for _, arg := range args {
		res = append(res, fmt.Sprint(arg))
	}
	return res
}

var gsm0338Encode map[rune]string = map[rune]string{
	'@':  "\x00",
	'£':  "\x01",
	'$':  "\x02",
	'¥':  "\x03",
	'è':  "\x04",
	'é':  "\x05",
	'ù':  "\x06",
	'ì':  "\x07",
	'ò':  "\x08",
	'Ç':  "\x09",
	'\r': "\x0a",
	'Ø':  "\x0b",
	'ø':  "\x0c",
	'\n': "\x0d",
	'Å':  "\x0e",
	'å':  "\x0f",
	'Δ':  "\x10",
	'_':  "\x11",
	'Φ':  "\x12",
	'Γ':  "\x13",
	'Λ':  "\x14",
	'Ω':  "\x15",
	'Π':  "\x16",
	'Ψ':  "\x17",
	'Σ':  "\x18",
	'Θ':  "\x19",
	'Ξ':  "\x1a",
	'Æ':  "\x1c",
	'æ':  "\x1d",
	'É':  "\x1f",
	'¤':  "\x24",
	'%':  "\x25",
	'¡':  "\x40",
	'Ä':  "\x5b",
	'Ö':  "\x5c",
	'Ñ':  "\x5d",
	'Ü':  "\x5e",
	'§':  "\x5f",
	'ä':  "\x7b",
	'ö':  "\x7c",
	'ñ':  "\x7d",
	'ü':  "\x7e",
	'à':  "\x7f",
	// escaped characters
	'€':  "\x1be",
	'[':  "\x1b<",
	'\\': "\x1b/",
	']':  "\x1b>",
	'^':  "\x1b^",
	'{':  "\x1b(",
	'|':  "\x1b@",
	'}':  "\x1b)",
	'~':  "\x1b=",
}

var gsm0338Decode map[rune]rune = map[rune]rune{
	'\x00': '@',
	'\x01': '£',
	'\x02': '$',
	'\x03': '¥',
	'\x04': 'è',
	'\x05': 'é',
	'\x06': 'ù',
	'\x07': 'ì',
	'\x08': 'ò',
	'\x09': 'Ç',
	'\x0a': '\r',
	'\x0b': 'Ø',
	'\x0c': 'ø',
	'\x0d': '\n',
	'\x0e': 'Å',
	'\x0f': 'å',
	'\x10': 'Δ',
	'\x11': '_',
	'\x12': 'Φ',
	'\x13': 'Γ',
	'\x14': 'Λ',
	'\x15': 'Ω',
	'\x16': 'Π',
	'\x17': 'Ψ',
	'\x18': 'Σ',
	'\x19': 'Θ',
	'\x1a': 'Ξ',
	'\x1c': 'Æ',
	'\x1d': 'æ',
	'\x1f': 'É',
	'\x24': '¤',
	'\x25': '%',
	'\x40': '¡',
	'\x5b': 'Ä',
	'\x5c': 'Ö',
	'\x5d': 'Ñ',
	'\x5e': 'Ü',
	'\x5f': '§',
	'\x7b': 'ä',
	'\x7c': 'ö',
	'\x7d': 'ñ',
	'\x7e': 'ü',
	'\x7f': 'à',
	// escaped characters
	// '\x1be': '€',
	// '\x1b<': '[',
	// '\x1b/': '\\',
	// '\x1b>': ']',
	// '\x1b^': '^',
	// '\x1b(': '{',
	// '\x1b@': '|',
	// '\x1b)': '}',
	// '\x1b=': '~',
}

// Encode the string to GSM03.38
func gsmEncode(s string) string {
	res := ""
	for _, c := range s {
		if d, ok := gsm0338Encode[c]; ok {
			res += string(d)
		} else {
			res += string(c)
		}
	}
	return res
}

// Decode the GSM03.38 to string
func gsmDecode(s string) string {
	res := ""
	for _, c := range s {
		if d, ok := gsm0338Decode[c]; ok {
			res += string(d)
		} else {
			res += string(c)
		}
	}
	return res
}

func Decode(s string) string {
	return gsmDecode(s)
}

// Encode the string to unicode
func unicodeEncode(s string) string {
	hex := fmt.Sprintf("%04x", utf16.Encode([]rune(s)))
	return strings.Replace(hex[1:len(hex)-1], " ", "", -1)
}

// A logging ReadWriteCloser for debugging
type LogReadWriteCloser struct {
	f io.ReadWriteCloser
}

func (self LogReadWriteCloser) Read(b []byte) (int, error) {
	n, err := self.f.Read(b)
	log.Printf("Read(%#v) = (%d, %v)\n", string(b[:n]), n, err)
	return n, err
}

func (self LogReadWriteCloser) Write(b []byte) (int, error) {
	n, err := self.f.Write(b)
	log.Printf("Write(%#v) = (%d, %v)\n", string(b), n, err)
	return n, err
}

func (self LogReadWriteCloser) Close() error {
	err := self.f.Close()
	log.Printf("Close() = %v\n", err)
	return err
}
