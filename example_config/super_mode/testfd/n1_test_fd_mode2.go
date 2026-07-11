package main
import (
    "fmt"
    "os"
    "errors"
    "time"
    "syscall"
    "os/signal"
	"strconv"
)


func ReadLoop(fileRX *os.File)  {
    buf := make([]byte,65535)
    for {
        size, err := fileRX.Read(buf)
        if err != nil{
            fmt.Println(err)
            return
        }
        fmt.Println("Go read fd" , fileRX.Fd() , ":" ,buf[:size])
    }
}
func WriteLoop(fileTX *os.File) {
    t := 0
    packet := ":gogogogogogogogogogogogo\n"
    for {
        fmt.Println("Go write fd:" , fileTX.Fd())
        _, err := fileTX.Write([]byte( strconv.Itoa(t) + packet))
        if err != nil {
            fmt.Println(err)
            return
        }
        t += 1
        time.Sleep(1 * time.Second)
    }
}

func cleanup(ftx *os.File,frx *os.File) {
    fmt.Println("Ctrl + C, exit")
    ftx.Close()
    frx.Close()
}

func main() {
	fdRXstr, has := os.LookupEnv("EG_FD_RX")
	if !has {
        fmt.Println(errors.New("Need Environment Variable EG_FD_RX"))
		return
	}
	fdRX, err := strconv.Atoi(fdRXstr)
	if err != nil {
        fmt.Println(err)
		return 
	}

	fdTxstr, has := os.LookupEnv("EG_FD_TX")
	if !has {
        fmt.Println(errors.New("Need Environment Variable EG_FD_TX"))
		return 
	}
	fdTX, err := strconv.Atoi(fdTxstr)
	if err != nil {
        fmt.Println(err)
		return 
	}

	fileRX := os.NewFile(uintptr(fdRX), "pipeRX")
	fileTX := os.NewFile(uintptr(fdTX), "pipeTX")
    go ReadLoop(fileRX)
    go WriteLoop(fileTX)
    c := make(chan os.Signal)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    <-c
    cleanup(fileRX,fileTX)
}

