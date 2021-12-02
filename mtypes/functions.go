package mtypes

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"time"
)

func S2TD(secs float64) time.Duration {
	return time.Duration(secs * float64(time.Second))
}

func RandomStr(length int, defaults string) string {
	bytes := make([]byte, length)

	if _, err := rand.Read(bytes); err != nil {
		if len(defaults) < length {
			defaults = fmt.Sprintf("%*s\n", length, defaults)
		}
		return defaults[:length]
	}

	for i, b := range bytes {
		bytes[i] = chars[b%byte(len(chars))]
	}

	return string(bytes)
}

func RandomBytes(length int, defaults []byte) []byte {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		copy(bytes, defaults)
		return defaults
	}
	return bytes
}

func ByteSlice2Byte32(bytes []byte) (ret [32]byte) {
	if len(bytes) != 32 {
		fmt.Println("Not a 32 len byte")
	}
	copy(ret[:], bytes)
	return
}

func Gzip(bytesIn []byte) (ret []byte) {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write([]byte(bytesIn))
	w.Close()
	return b.Bytes()
}

func GUzip(bytesIn []byte) (ret []byte, err error) {
	b := bytes.NewReader(bytesIn)
	r, err := gzip.NewReader(b)
	if err != nil {
		return
	}
	return ioutil.ReadAll(r)
}
