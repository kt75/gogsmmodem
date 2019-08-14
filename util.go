package gogsmmodem

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

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

var gsm0338 map[rune]string = map[rune]string{
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
	' ':  " ",
	'!':  "!",
	'"':  "\"",
	'#':  "#",
	'¤':  "\x24",
	'%':  "\x25",
	'&':  "&",
	'\'': "'",
	'(':  "(",
	')':  ")",
	'*':  "*",
	'+':  "+",
	',':  ",",
	'-':  "-",
	'.':  ".",
	'/':  "/",
	'0':  "0",
	'1':  "1",
	'2':  "2",
	'3':  "3",
	'4':  "4",
	'5':  "5",
	'6':  "6",
	'7':  "7",
	'8':  "8",
	'9':  "9",
	':':  ":",
	';':  ";",
	'<':  "<",
	'=':  "=",
	'>':  ">",
	'?':  "?",
	'¡':  "\x40",
	'A':  "A",
	'B':  "B",
	'C':  "C",
	'D':  "D",
	'E':  "E",
	'F':  "F",
	'G':  "G",
	'H':  "H",
	'I':  "I",
	'J':  "J",
	'K':  "K",
	'L':  "L",
	'M':  "M",
	'N':  "N",
	'O':  "O",
	'P':  "P",
	'Q':  "Q",
	'R':  "R",
	'S':  "S",
	'T':  "T",
	'U':  "U",
	'V':  "V",
	'W':  "W",
	'X':  "X",
	'Y':  "Y",
	'Z':  "Z",
	'Ä':  "\x5b",
	'Ö':  "\x5c",
	'Ñ':  "\x5d",
	'Ü':  "\x5e",
	'§':  "\x5f",
	'a':  "a",
	'b':  "b",
	'c':  "c",
	'd':  "d",
	'e':  "e",
	'f':  "f",
	'g':  "g",
	'h':  "h",
	'i':  "i",
	'j':  "j",
	'k':  "k",
	'l':  "l",
	'm':  "m",
	'n':  "n",
	'o':  "o",
	'p':  "p",
	'q':  "q",
	'r':  "r",
	's':  "s",
	't':  "t",
	'u':  "u",
	'v':  "v",
	'w':  "w",
	'x':  "x",
	'y':  "y",
	'z':  "z",
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

var unicode map[rune]string = map[rune]string{
	'@':  "0040",
	'$':  "0024",
	'¥':  "00A5",
	'\r': "000A",
	'\n': "000D",
	'_':  "005F",
	' ':  "0020",
	'!':  "0021",
	'"':  "0022",
	'#':  "0023",
	'%':  "0025",
	'&':  "0026",
	'\'': "0027",
	'(':  "0028",
	')':  "0029",
	'*':  "002A",
	'+':  "002B",
	',':  "002C",
	'-':  "002D",
	'.':  "002E",
	'/':  "002F",
	'0':  "0030",
	'1':  "0031",
	'2':  "0032",
	'3':  "0033",
	'4':  "0034",
	'5':  "0035",
	'6':  "0036",
	'7':  "0037",
	'8':  "0038",
	'9':  "0039",
	':':  "003A",
	';':  "003B",
	'<':  "003C",
	'=':  "003D",
	'>':  "003E",
	'?':  "003F",
	'A':  "0041",
	'B':  "0042",
	'C':  "0043",
	'D':  "0044",
	'E':  "0045",
	'F':  "0046",
	'G':  "0047",
	'H':  "0048",
	'I':  "0049",
	'J':  "004A",
	'K':  "004B",
	'L':  "004C",
	'M':  "004D",
	'N':  "004E",
	'O':  "004F",
	'P':  "0050",
	'Q':  "0051",
	'R':  "0052",
	'S':  "0053",
	'T':  "0054",
	'U':  "0055",
	'V':  "0056",
	'W':  "0057",
	'X':  "0058",
	'Y':  "0059",
	'Z':  "005A",
	'a':  "0061",
	'b':  "0062",
	'c':  "0063",
	'd':  "0064",
	'e':  "0065",
	'f':  "0066",
	'g':  "0067",
	'h':  "0068",
	'i':  "0069",
	'j':  "006A",
	'k':  "006B",
	'l':  "006C",
	'm':  "006D",
	'n':  "006E",
	'o':  "006F",
	'p':  "0070",
	'q':  "0071",
	'r':  "0072",
	's':  "0073",
	't':  "0074",
	'u':  "0075",
	'v':  "0076",
	'w':  "0077",
	'x':  "0078",
	'y':  "0079",
	'z':  "007A",
	// escaped characters
	'[':  "005B",
	'\\': "005C",
	']':  "005D",
	'{':  "007B",
	'|':  "007C",
	'}':  "007D",
	'~':  "007E",
	// persian characters
	'ا': "0627",
	'آ': "0622",
	'ب': "0628",
	'پ': "067E",
	'ت': "062A",
	'ث': "062B",
	'ج': "062C",
	'چ': "0686",
	'ح': "062D",
	'خ': "062E",
	'د': "062F",
	'ذ': "0630",
	'ر': "0631",
	'ز': "0632",
	'ژ': "0698",
	'س': "0633",
	'ش': "0634",
	'ص': "0635",
	'ض': "0636",
	'ط': "0637",
	'ظ': "0638",
	'ع': "0639",
	'غ': "063A",
	'ف': "0641",
	'ق': "0642",
	'ک': "0643",
	'گ': "06AF",
	'ل': "0644",
	'م': "0645",
	'ن': "0646",
	'و': "0648",
	'ه': "0647",
	'ی': "06CC",
	'۰': "0660",
	'۱': "0661",
	'۲': "0662",
	'۳': "0663",
	'۴': "0664",
	'۵': "0665",
	'۶': "0666",
	'۷': "0667",
	'۸': "0668",
	'۹': "0669",
}

// Encode the string to GSM03.38
func gsmEncode(s string) string {
	res := ""
	for _, c := range s {
		if d, ok := gsm0338[c]; ok {
			res += string(d)
		}
	}
	return res
}

// Encode the string to unicode
func unicodeEncode(s string) string {
	res := ""
	for _, c := range s {
		if d, ok := unicode[c]; ok {
			res += string(d)
		}
	}
	return res
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
