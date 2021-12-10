package mtypes

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	nonSecureRand "math/rand"
	"strconv"
	"time"

	"gopkg.in/yaml.v2"
)

func S2TD(secs float64) time.Duration {
	return time.Duration(secs * float64(time.Second))
}

func String2Float64(s string) (float64, error) {
	if s == "Inf" {
		return Infinity, nil
	}
	return strconv.ParseFloat(s, 64)
}

func String2NodeID(s string) (Vertex, error) {
	ret, err := strconv.ParseUint(s, 10, 16)
	return Vertex(ret), err
}

func RandomStr(length int, defaults string) (ret string) {
	bytes := RandomBytes(length, []byte(defaults))

	for i, b := range bytes {
		bytes[i] = chars[b%byte(len(chars))]
	}
	ret = string(bytes)

	return

}

func RandomBytes(length int, defaults []byte) (ret []byte) {
	var err error
	ret = make([]byte, length)

	_, err = rand.Read(ret)
	if err == nil {
		return
	}
	_, err = nonSecureRand.Read(ret)
	if err == nil {
		return
	}

	if len(defaults) < length {
		copy(ret, defaults[:length])
	}
	return
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

func ReadYaml(filePath string, out interface{}) (err error) {
	yamlFile, err := ioutil.ReadFile(filePath)
	if err != nil {
		return
	}
	err = yaml.Unmarshal(yamlFile, out)
	return
}

func AbsInt(a int) int {
	if a < 0 {
		a *= -1
	}
	return a
}
