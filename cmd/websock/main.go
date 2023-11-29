package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/jpeg"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kbinani/screenshot"
)

var (
	upgrader = websocket.Upgrader{}
	frameNum int64
)

func captureScreen() []byte {
	screen, err := screenshot.CaptureDisplay(0)
	if err != nil {
		panic(err)
	}

	buf := new(bytes.Buffer)
	err = jpeg.Encode(buf, screen, &jpeg.Options{Quality: 75})
	if err != nil {
		panic(err)
	}

	imgBase64 := make([]byte, base64.StdEncoding.EncodedLen(len(buf.Bytes())))
	base64.StdEncoding.Encode(imgBase64, buf.Bytes())
	return imgBase64
}

func screenHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("Upgrade error: ", err)
		return
	}
	defer conn.Close()

	lastTime := time.Now()
	ticker := time.NewTicker(time.Second)

	var exit atomic.Bool

	go func() {
		for range ticker.C {
			frames := atomic.SwapInt64(&frameNum, 0)
			elapsed := time.Since(lastTime)
			lastTime = time.Now()

			log.Printf("FPS: %v", float64(frames)/elapsed.Seconds())

			if exit.Load() {
				break
			}
		}
	}()

	var mu sync.Mutex

	for range time.Tick(33 * time.Millisecond) {
		go func() {
			atomic.AddInt64(&frameNum, 1)

			imgBytes := captureScreen()

			mu.Lock()
			defer mu.Unlock()
			err = conn.WriteMessage(websocket.TextMessage, imgBytes)
			if err != nil {
				log.Println("Write error: ", err)
				exit.Store(true)
			}
		}()
		if exit.Load() {
			break
		}
	}
}

func displaysHandler(w http.ResponseWriter, r *http.Request) {
	numDisplays := screenshot.NumActiveDisplays()
	for i := 0; i < numDisplays; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		fmt.Fprintf(w, "Display %d: %dx%d+%d+%d\n", i, bounds.Dx(), bounds.Dy(), bounds.Min.X, bounds.Min.Y)
	}
}

func main() {
	// Получаем IP-адрес на внешнем интерфейсе
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Fatal(err)
	}

	var ip net.IP
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ip = ipnet.IP
				break
			}
		}
	}

	if ip == nil {
		log.Fatal("Could not determine IP address on external interface")
	}

	log.Printf("Server started at http://%s\n", ip)

	http.HandleFunc("/displays", displaysHandler)
	http.HandleFunc("/screen", screenHandler)
	http.Handle("/", http.FileServer(http.Dir("./static")))

	log.Fatal(http.ListenAndServe("0.0.0.0:80", nil))
}
