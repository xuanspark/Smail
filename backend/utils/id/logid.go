package id

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

var (
	ipInstance string
	ipOnce     sync.Once
	ipMu       sync.RWMutex
)

func getLocalIP() string {
	ipOnce.Do(func() {
		ip := "127.0.0.1"
		addrs, err := net.InterfaceAddrs()
		if err == nil {
			for _, a := range addrs {
				if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
					if ipnet.IP.To4() != nil {
						ip = ipnet.IP.String()
						break
					}
				}
			}
		}
		ipMu.Lock()
		ipInstance = ip
		ipMu.Unlock()
	})

	ipMu.RLock()
	defer ipMu.RUnlock()
	return ipInstance
}

func GenLogID() string {
	ip := getLocalIP()

	now := time.Now()
	timeNano := now.UnixNano()
	pid := os.Getpid()
	randomBytes := make([]byte, 3)
	if _, err := rand.Read(randomBytes); err != nil {
		randomBytes = []byte{byte(timeNano), byte(timeNano >> 8), byte(pid)}
	}
	b := bytes.Buffer{}

	b.WriteString(hex.EncodeToString(net.ParseIP(ip).To4()))
	b.WriteString(fmt.Sprintf("%x", now.Unix()&0xffffffff))
	b.WriteString(fmt.Sprintf("%04x", timeNano&0xffff))
	b.WriteString(fmt.Sprintf("%04x", pid&0xffff))
	b.WriteString(hex.EncodeToString(randomBytes))
	b.WriteString("b0")

	return b.String()
}
