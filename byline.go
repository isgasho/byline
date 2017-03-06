package byline

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"regexp"
	"strings"
)

var (
	// ErrOmitLine - error for Map*Err/AWKMode, for omitting current line
	ErrOmitLine = errors.New("ErrOmitLine")

	// default field separator
	defaultFS = regexp.MustCompile(`\s+`)
	// default line separator
	defaultRS byte = '\n'
	// for Grep* methods
	nullBytes = []byte{}
)

// Reader - line by line Reader
type Reader struct {
	scanner     *bufio.Scanner
	filterFuncs []func(line []byte) ([]byte, error)
	awkVars     AWKVars
}

// AWKVars - settings for AWK mode, see man awk
type AWKVars struct {
	NR int            // number of current line (begin from 1)
	NF int            // fields count in curent line
	RS byte           // record separator, default is '\n'
	FS *regexp.Regexp // field separator, default is `\s+`
}

// NewReader - get new line by line Reader
func NewReader(reader io.Reader) *Reader {
	lr := &Reader{
		scanner: bufio.NewScanner(reader),
		awkVars: AWKVars{
			RS: defaultRS,
			FS: defaultFS,
		},
	}

	lr.scanner.Split(lr.scanLinesWithNL)
	return lr
}

func (lr *Reader) scanLinesWithNL(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, lr.awkVars.RS); i >= 0 {
		// We have a full newline-terminated line.
		return i + 1, data[0 : i+1], nil
	}
	// If we're at EOF, we have a final, non-terminated line. Return it.
	if atEOF {
		return len(data), data, nil
	}

	// Request more data.
	return 0, nil, nil
}

// Read - implement io.Reader interface
func (lr *Reader) Read(p []byte) (n int, err error) {
	var (
		bufErr    error
		lineBytes []byte
	)
	if lr.scanner.Scan() {
		lineBytes = lr.scanner.Bytes()
		lr.awkVars.NR++

		for _, filterFunc := range lr.filterFuncs {
			var filterErr error
			lineBytes, filterErr = filterFunc(lineBytes)
			if filterErr != nil {
				switch {
				case filterErr == ErrOmitLine:
					lineBytes = nullBytes
				case filterErr != nil:
					bufErr = filterErr
				}
				break
			}
		}
	} else {
		bufErr = lr.scanner.Err()
		lineBytes = nullBytes
		if bufErr == nil {
			bufErr = io.EOF
		}
	}

	copy(p, lineBytes)
	return len(lineBytes), bufErr
}

// Map - set filter function for process each line
func (lr *Reader) Map(filterFn func([]byte) []byte) *Reader {
	return lr.MapErr(func(line []byte) ([]byte, error) {
		return filterFn(line), nil
	})
}

// MapErr - set filter function for process each line, returns error if needed (io.EOF for example)
func (lr *Reader) MapErr(filterFn func([]byte) ([]byte, error)) *Reader {
	lr.filterFuncs = append(lr.filterFuncs, filterFn)
	return lr
}

// MapString - set filter function for process each line as string
func (lr *Reader) MapString(filterFn func(string) string) *Reader {
	return lr.MapErr(func(line []byte) ([]byte, error) {
		return []byte(filterFn(string(line))), nil
	})
}

// MapStringErr - set filter function for process each line as string, returns error if needed (io.EOF for example)
func (lr *Reader) MapStringErr(filterFn func(string) (string, error)) *Reader {
	return lr.MapErr(func(line []byte) ([]byte, error) {
		newString, err := filterFn(string(line))
		return []byte(newString), err
	})
}

// Grep - grep lines by func
func (lr *Reader) Grep(filterFn func([]byte) bool) *Reader {
	return lr.MapErr(func(line []byte) ([]byte, error) {
		if filterFn(line) {
			return line, nil
		}

		return nullBytes, ErrOmitLine
	})
}

// GrepString - grep lines as string by func
func (lr *Reader) GrepString(filterFn func(string) bool) *Reader {
	return lr.Grep(func(line []byte) bool {
		return filterFn(string(line))
	})
}

// GrepByRegexp - grep lines by regexp
func (lr *Reader) GrepByRegexp(re *regexp.Regexp) *Reader {
	return lr.Grep(func(line []byte) bool {
		return re.Match(line)
	})
}

// SetRS - set lines (records) separator
func (lr *Reader) SetRS(rs byte) *Reader {
	lr.awkVars.RS = rs
	return lr
}

// SetFS - set field separator for AWK mode
func (lr *Reader) SetFS(fs *regexp.Regexp) *Reader {
	lr.awkVars.FS = fs
	return lr
}

// AWKMode - process lines with AWK like mode
func (lr *Reader) AWKMode(filterFn func(line string, fields []string, vars AWKVars) (string, error)) *Reader {
	return lr.MapStringErr(func(line string) (string, error) {
		addRS := false
		if strings.HasSuffix(line, string(lr.awkVars.RS)) {
			addRS = true
			line = strings.TrimSuffix(line, string(lr.awkVars.RS))
		}

		fields := lr.awkVars.FS.Split(line, -1)
		lr.awkVars.NF = len(fields)
		result, err := filterFn(line, fields, lr.awkVars)
		if err != nil {
			return "", err
		}

		if !strings.HasSuffix(result, string(lr.awkVars.RS)) && addRS {
			result += string(lr.awkVars.RS)
		}
		return result, nil
	})
}

// Discard - read all content from Reader for side effect from filter functions
func (lr *Reader) Discard() error {
	_, err := io.Copy(ioutil.Discard, lr)
	return err
}

// ReadAllSlice - read all content from Reader by lines to slice of []byte
func (lr *Reader) ReadAllSlice() ([][]byte, error) {
	result := [][]byte{}
	err := lr.Map(func(line []byte) []byte {
		result = append(result, line)
		return nullBytes
	}).Discard()

	return result, err
}

// ReadAll - read all content from Reader to slice of bytes
func (lr *Reader) ReadAll() ([]byte, error) {
	return ioutil.ReadAll(lr)
}

// ReadAllSliceString - read all content from Reader to string slice by lines
func (lr *Reader) ReadAllSliceString() ([]string, error) {
	result := []string{}
	err := lr.MapString(func(line string) string {
		result = append(result, line)
		return ""
	}).Discard()

	return result, err
}

// ReadAllString - read all content from Reader to one string
func (lr *Reader) ReadAllString() (string, error) {
	result, err := ioutil.ReadAll(lr)
	return string(result), err
}
