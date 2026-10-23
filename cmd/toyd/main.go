// toyd is the toy service ari looks after in the demo and tests.
// SIGUSR1 starts a simulated memory leak that slows /health down.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	config := flag.String("config", "toyd.conf", "config file")
	pidfile := flag.String("pidfile", "toyd.pid", "pid file")
	flag.Parse()

	conf, err := readConfig(*config)
	if err != nil {
		log.Fatal(err)
	}
	port := conf["port"]
	if port == "" {
		log.Fatal("toyd: no port in config")
	}
	// The pidfile goes down only once the port is ours, so a toyd that
	// cannot listen never looks alive.
	l, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*pidfile, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		log.Fatal(err)
	}

	var mu sync.Mutex
	var leak [][]byte
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGUSR1)
	go func() {
		<-sig
		for {
			mu.Lock()
			if len(leak) < 500 {
				leak = append(leak, make([]byte, 1<<20))
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
		}
	}()

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		leaked := len(leak)
		mu.Unlock()
		time.Sleep(time.Duration(leaked) * 10 * time.Millisecond)

		c, err := readConfig(*config)
		if err != nil || c["status"] != "ok" {
			http.Error(w, "bad config", http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	log.Fatal(http.Serve(l, nil))
}

// readConfig parses key=value lines; blank lines and # comments are skipped.
func readConfig(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("toyd: bad config line %q", line)
		}
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return m, nil
}
