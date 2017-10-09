package serial

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"time"
)

// End of line character (AKA EOL), newline character (ASCII 10, CR, '\n'). is used by default.
const EOL_DEFAULT byte = '\n'

/*******************************************************************************************
*******************************   TYPE DEFINITIONS 	****************************************
*******************************************************************************************/
const DefaultSize = 8 // Default value for Config.Size

type StopBits byte
type Parity byte

const (
	Stop1     StopBits = 1
	Stop1Half StopBits = 15
	Stop2     StopBits = 2
)

const (
	ParityNone  Parity = 'N'
	ParityOdd   Parity = 'O'
	ParityEven  Parity = 'E'
	ParityMark  Parity = 'M' // parity bit is always 1
	ParitySpace Parity = 'S' // parity bit is always 0
)

type Config struct {
	Name        string
	Baud        int
	ReadTimeout time.Duration // Total timeout

	// Size is the number of data bits. If 0, DefaultSize is used.
	Size byte

	// Parity is the bit to use and defaults to ParityNone (no parity bit).
	Parity Parity

	// Number of stop bits to use. Default is 1 (1 stop bit).
	StopBits StopBits

	// RTSFlowControl bool
	// DTRFlowControl bool
	// XONFlowControl bool

	// CRLFTranslate bool
}

type SerialPort struct {
	port          io.ReadWriteCloser
	name          string
	baud          int
	eol           uint8
	rxChar        chan byte
	closeReqChann chan bool
	closeAckChann chan error
	buff          *bytes.Buffer
	logger        *log.Logger
	portIsOpen    bool
	Verbose       bool
	// openPort      func(port string, baud int) (io.ReadWriteCloser, error)
}

// ErrBadSize is returned if Size is not supported.
var ErrBadSize error = errors.New("unsupported serial data size")

// ErrBadStopBits is returned if the specified StopBits setting not supported.
var ErrBadStopBits error = errors.New("unsupported stop bit setting")

// ErrBadParity is returned if the parity is not supported.
var ErrBadParity error = errors.New("unsupported parity setting")

/*******************************************************************************************
********************************   BASIC FUNCTIONS  ****************************************
*******************************************************************************************/

func New() *SerialPort {
	y, m, d := time.Now().Date()
	fname := fmt.Sprintf(".//log//sms_%04d%02d%02d.txt", y, m, d)
	b, err := PathExists(fname)
	var file *os.File
	if b {
		//Oen file
		file, err = os.OpenFile(fname, os.O_WRONLY|os.O_APPEND, 0666)
	} else {
		// Create new file
		file, err = os.OpenFile(fname, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	}
	if err != nil {
		log.Fatalln("Failed to open log file", ":", err)
	}

	multi := io.MultiWriter(file, os.Stdout)
	return &SerialPort{
		logger:  log.New(multi, "PREFIX: ", log.Ldate|log.Ltime),
		eol:     EOL_DEFAULT,
		buff:    bytes.NewBuffer(make([]uint8, 256)),
		Verbose: true,
	}
}

//Create a connection with I/O device by serial com port
//@name: COM1 - COM24
//@baud: 9600/38400/115200...
//@databits:"5"/"6"/"7"/"8"   DefaultSize = 8
//@timeout: 1m = 1minutes / 1h = 1 hour  according to parser rule of offical TIME package
//@parity: "N" = none / "O" = odd / "E" = even / "M" = mark / "S" = space
//@stopbit: "1" = 1 bit / "1.5" = 1 half bit / "2" = 2 bits
func (sp *SerialPort) Open(name string, baud int, databits, timeout, parity, stopbit string) error {
	// Check if port is open
	if sp.portIsOpen {
		return fmt.Errorf("\"%s\" is already open", name)
	}

	var serialCfg Config

	if len(name) != 0 {
		serialCfg.Name = name
	}

	if baud != 0 {
		serialCfg.Baud = baud
	}

	if len(parity) != 0 {
		switch parity {
		case "N":
			serialCfg.Parity = ParityNone
		case "O":
			serialCfg.Parity = ParityOdd
		case "E":
			serialCfg.Parity = ParityEven
		case "M":
			serialCfg.Parity = ParityMark
		case "S":
			serialCfg.Parity = ParitySpace
		default:
			return errors.New("Invalid parity")
		}
	}

	if len(timeout) != 0 {
		to, err := time.ParseDuration(timeout)
		if err != nil {
			return errors.New("Invalid time out params")
		}
		serialCfg.ReadTimeout = to
	}

	if len(stopbit) != 0 {
		switch stopbit {
		case "1":
			serialCfg.StopBits = Stop1
		case "1.5":
			serialCfg.StopBits = Stop1Half
		case "2":
			serialCfg.StopBits = Stop2
		default:
			return errors.New("Invalid stop bits")
		}
	}

	var databit byte
	switch databits {
	case "5":
		databit = 5
	case "6":
		databit = 6
	case "7":
		databit = 7
	case "8":
		databit = DefaultSize
	default:
		databit = DefaultSize
	}

	// Open serial port
	comPort, err := openPort(serialCfg.Name,
		serialCfg.Baud,
		databit,
		serialCfg.Parity,
		serialCfg.StopBits,
		serialCfg.ReadTimeout)
	if err != nil {
		return fmt.Errorf("Unable to open port \"%s\" - %s", name, err)
	}

	// Open port succesfull
	sp.name = name
	sp.baud = baud
	sp.port = comPort
	sp.portIsOpen = true
	sp.buff.Reset()
	// Open channels
	sp.rxChar = make(chan byte)
	// Enable threads
	go sp.readSerialPort()
	go sp.processSerialPort()
	sp.logger.SetPrefix(fmt.Sprintf("[%s] ", sp.name))
	sp.log("Serial port %s@%d open", sp.name, sp.baud)
	return nil
}

// This method close the current Serial Port.
func (sp *SerialPort) Close() error {
	if sp.portIsOpen {
		sp.portIsOpen = false
		close(sp.rxChar)
		sp.log("Serial port %s closed", sp.name)
		return sp.port.Close()
	}
	return nil
}

// This method prints data trough the serial port.
func (sp *SerialPort) Write(data []byte) (n int, err error) {
	if sp.portIsOpen {
		n, err = sp.port.Write(data)
		if err != nil {
			// Do nothing
		} else {
			sp.log("Tx >> %s", string(data))
		}
	} else {
		err = fmt.Errorf("Serial port is not open")
	}
	return
}

// This method prints data trough the serial port.
func (sp *SerialPort) Print(str string) error {
	if sp.portIsOpen {
		_, err := sp.port.Write([]byte(str))
		if err != nil {
			return err
		} else {
			sp.log("Tx >> %s", str)
		}
	} else {
		return fmt.Errorf("Serial port is not open")
	}
	return nil
}

// Prints data to the serial port as human-readable ASCII text followed by a carriage return character
// (ASCII 13, CR, '\r') and a newline character (ASCII 10, LF, '\n').
func (sp *SerialPort) Println(str string) error {
	return sp.Print(str + "\r\n")
}

// Printf formats according to a format specifier and print data trough the serial port.
func (sp *SerialPort) Printf(format string, args ...interface{}) error {
	str := format
	if len(args) > 0 {
		str = fmt.Sprintf(format, args...)
	}
	return sp.Print(str)
}

//This method send a binary file trough the serial port. If EnableLog is active then this method will log file related data.
func (sp *SerialPort) SendFile(filepath string) error {
	// Aux Vars
	sentBytes := 0
	q := 512
	data := []byte{}
	// Read file
	file, err := ioutil.ReadFile(filepath)
	if err != nil {
		sp.log("DBG >> %s", "Invalid filepath")
		return err
	} else {
		fileSize := len(file)
		sp.log("INF >> %s", "File size is %d bytes", fileSize)

		for sentBytes <= fileSize {
			//Try sending slices of less or equal than 512 bytes at time
			if len(file[sentBytes:]) > q {
				data = file[sentBytes:(sentBytes + q)]
			} else {
				data = file[sentBytes:]
			}
			// Write binaries
			_, err := sp.port.Write(data)
			if err != nil {
				sp.log("DBG >> %s", "Error while sending the file")
				return err
			} else {
				sentBytes += q
				time.Sleep(time.Millisecond * 100)
			}
		}
	}
	//Encode data to send
	return nil
}

// Read the first byte of the serial buffer.
func (sp *SerialPort) Read() (byte, error) {
	if sp.portIsOpen {
		return sp.buff.ReadByte()
	} else {
		return 0x00, fmt.Errorf("Serial port is not open")
	}
	return 0x00, nil
}

// Read first available line from serial port buffer.
//
// Line is delimited by the EOL character, newline character (ASCII 10, LF, '\n') is used by default.
//
// The text returned from ReadLine does not include the line end ("\r\n" or '\n').
func (sp *SerialPort) ReadLine() (string, error) {
	if sp.portIsOpen {
		line, err := sp.buff.ReadString(sp.eol)
		if err != nil {
			return "", err
		} else {
			return removeEOL(line), nil
		}
	} else {
		return "", fmt.Errorf("Serial port is not open")
	}
	return "", nil
}

// Wait for a defined regular expression for a defined amount of time.
func (sp *SerialPort) WaitForRegexTimeout(exp string, timeout time.Duration) (string, error) {

	if sp.portIsOpen {
		//Decode received data
		timeExpired := false

		regExpPatttern := regexp.MustCompile(exp)

		//Timeout structure
		c1 := make(chan string, 1)
		go func() {
			sp.log("INF >> Waiting for RegExp: \"%s\"", exp)
			result := []string{}
			for !timeExpired {
				time.Sleep(time.Millisecond * 50)
				line, err := sp.ReadLine()
				if err != nil {
					// Do nothing
				} else {
					result = regExpPatttern.FindAllString(line, -1)
					if len(result) > 0 {
						c1 <- result[0]
						break
					}
				}
			}
		}()
		select {
		case data := <-c1:
			sp.log("INF >> The RegExp: \"%s\"", exp)
			sp.log("INF >> Has been matched: \"%s\"", data)
			return data, nil
		case <-time.After(timeout):
			timeExpired = true
			sp.log("INF >> Unable to match RegExp: \"%s\"", exp)
			return "", fmt.Errorf("Timeout expired")
		}
	} else {
		return "", fmt.Errorf("Serial port is not open")
	}
	return "", nil
}

// Available return the total number of available unread bytes on the serial buffer.
func (sp *SerialPort) Available() int {
	return sp.buff.Len()
}

// Change end of line character (AKA EOL), newline character (ASCII 10, LF, '\n') is used by default.
func (sp *SerialPort) EOL(c byte) {
	sp.eol = c
}

/*******************************************************************************************
******************************   PRIVATE FUNCTIONS  ****************************************
*******************************************************************************************/

func (sp *SerialPort) readSerialPort() {
	rxBuff := make([]byte, 256)
	for sp.portIsOpen {
		n, _ := sp.port.Read(rxBuff)
		// Write data to serial buffer
		sp.buff.Write(rxBuff[:n])
		for _, b := range rxBuff[:n] {
			if sp.portIsOpen {
				sp.rxChar <- b
			}
		}
	}
}

func (sp *SerialPort) processSerialPort() {
	screenBuff := make([]byte, 0)
	var lastRxByte byte
	for {
		if sp.portIsOpen {
			lastRxByte = <-sp.rxChar
			// Print received lines
			switch lastRxByte {
			case sp.eol:
				// EOL - Print received data
				sp.log("Rx << %s", string(append(screenBuff, lastRxByte)))
				screenBuff = make([]byte, 0) //Clean buffer
				break
			default:
				screenBuff = append(screenBuff, lastRxByte)
			}
		} else {
			break
		}
	}
}

func (sp *SerialPort) log(format string, a ...interface{}) {
	if sp.Verbose {
		sp.logger.Printf(format, a...)
	}
}

func removeEOL(line string) string {
	var data []byte
	// Remove CR byte "\r"
	for _, b := range []byte(line) {
		switch b {
		case '\r':
			// Do nothing
		case '\n':
			// Do nothing
		default:
			data = append(data, b)
		}
	}
	return string(data)
}

// Converts the timeout values for Linux / POSIX systems
func posixTimeoutValues(readTimeout time.Duration) (vmin uint8, vtime uint8) {
	const MAXUINT8 = 1<<8 - 1 // 255
	// set blocking / non-blocking read
	var minBytesToRead uint8 = 1
	var readTimeoutInDeci int64
	if readTimeout > 0 {
		// EOF on zero read
		minBytesToRead = 0
		// convert timeout to deciseconds as expected by VTIME
		readTimeoutInDeci = (readTimeout.Nanoseconds() / 1e6 / 100)
		// capping the timeout
		if readTimeoutInDeci < 1 {
			// min possible timeout 1 Deciseconds (0.1s)
			readTimeoutInDeci = 1
		} else if readTimeoutInDeci > MAXUINT8 {
			// max possible timeout is 255 deciseconds (25.5s)
			readTimeoutInDeci = MAXUINT8
		}
	}
	return minBytesToRead, uint8(readTimeoutInDeci)
}

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
