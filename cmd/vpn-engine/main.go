package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type Config struct {
	Mode          string `json:"mode"` // direct, payload, tls, payload_tls
	SSHHost       string `json:"ssh_host"`
	SSHPort       int    `json:"ssh_port"`
	SSHUser       string `json:"ssh_user"`
	SSHPassword   string `json:"ssh_password"`
	SSHPrivateKey string `json:"ssh_private_key"`
	AuthMethod    string `json:"auth_method"` // password or privatekey
	Compression   bool   `json:"compression"`
	Payload       string `json:"payload"`
	ProxyHost     string `json:"proxy_host"`
	ProxyPort     int    `json:"proxy_port"`
	SNI           string `json:"sni"`
	SOCKSHost     string `json:"socks_host"`
	SOCKSPort     int    `json:"socks_port"`
	WebHost       string `json:"web_host"`
	WebPort       int    `json:"web_port"`
}

var (
	mu            sync.Mutex
	cfg           Config
	sshClient     *ssh.Client
	socksListener net.Listener
	running       bool
	logLines      []string
	root          = moduleRoot()
)

func moduleRoot() string {
	if v := os.Getenv("VPN_MODULE_DIR"); v != "" {
		return v
	}
	return "/data/adb/modules/http_custom_headers_vpn"
}
func proxyControl(action string) error {
	script := filepath.Join(moduleRoot(), "scripts", "proxyctl.sh")
	cmd := exec.Command("/system/bin/sh", script, action)
	cmd.Env = append(os.Environ(), "VPN_MODULE_DIR="+moduleRoot())
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		logf("proxyctl %s: %s", action, strings.TrimSpace(string(out)))
	}
	return err
}
func defaults() Config {
	return Config{Mode: "payload", SSHPort: 443, AuthMethod: "password", Payload: "CONNECT [host]:[port] HTTP/1.1[crlf]Host: [host][crlf]Connection: keep-alive[crlf][crlf]", SOCKSHost: "127.0.0.1", SOCKSPort: 1080, WebHost: "127.0.0.1", WebPort: 8787}
}
func logf(f string, a ...any) {
	s := time.Now().Format("15:04:05") + " " + fmt.Sprintf(f, a...)
	mu.Lock()
	defer mu.Unlock()
	logLines = append(logLines, s)
	if len(logLines) > 500 {
		logLines = logLines[len(logLines)-500:]
	}
	log.Println(s)
}
func cfgPath() string { return filepath.Join(moduleRoot(), "cfgs", "config.json") }
func loadConfig() {
	cfg = defaults()
	b, e := os.ReadFile(cfgPath())
	if e == nil {
		_ = json.Unmarshal(b, &cfg)
	}
}
func saveConfig(c Config) error {
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(cfgPath()), 0755); e != nil {
		return e
	}
	return os.WriteFile(cfgPath(), b, 0600)
}

// bufferedConn preserves bytes already read while parsing a proxy response.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

// expandPayload replaces supported tokens. [split] is handled as a write
// boundary by writePayload, rather than being sent literally to the server.
func expandPayload(payload string, c Config) []string {
	parts := strings.Split(payload, "[split]")
	replacer := strings.NewReplacer(
		"[crlf*2]", "\r\n\r\n",
		"[crlf]", "\r\n",
		"[cr]", "\r",
		"[lf]", "\n",
		"[host]", c.SSHHost,
		"[port]", strconv.Itoa(c.SSHPort),
		"[ua]", "Dalvik/2.1.0",
		"[protocol]", "HTTP/1.1",
	)
	for i := range parts {
		parts[i] = replacer.Replace(parts[i])
	}
	return parts
}

func writePayload(conn net.Conn, payload string, c Config) error {
	parts := expandPayload(payload, c)
	for i, part := range parts {
		if part == "" {
			continue
		}
		if _, err := io.WriteString(conn, part); err != nil {
			return fmt.Errorf("gagal mengirim bagian payload %d: %w", i+1, err)
		}
		// TCP is a byte stream: [split] separates writes, not guaranteed packets.
		if i < len(parts)-1 {
			time.Sleep(150 * time.Millisecond)
		}
	}
	return nil
}

// consumeProxyResponse reads an HTTP proxy response while preserving any
// buffered bytes that belong to the SSH stream.
func consumeProxyResponse(conn net.Conn) (net.Conn, error) {
	reader := bufio.NewReaderSize(conn, 4096)
	firstLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("menunggu respons proxy: %w", err)
	}

	if strings.HasPrefix(firstLine, "SSH-") {
		return &bufferedConn{Conn: conn, reader: reader}, nil
	}
	if !strings.HasPrefix(firstLine, "HTTP/") {
		return nil, fmt.Errorf("respons proxy bukan HTTP: %q", strings.TrimSpace(firstLine))
	}

	fields := strings.Fields(firstLine)
	if len(fields) < 2 {
		return nil, fmt.Errorf("status HTTP proxy tidak valid: %q", strings.TrimSpace(firstLine))
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, fmt.Errorf("status HTTP proxy tidak valid: %q", strings.TrimSpace(firstLine))
	}

	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			return nil, fmt.Errorf("header respons proxy tidak lengkap: %w", readErr)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	if status != http.StatusOK && status != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("proxy menolak payload: %s", strings.TrimSpace(firstLine))
	}
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

func dialTunnel(c Config) (net.Conn, error) {
	targetHost, targetPort := c.SSHHost, c.SSHPort
	if c.ProxyHost != "" && c.ProxyPort > 0 {
		targetHost, targetPort = c.ProxyHost, c.ProxyPort
	}
	conn, err := net.DialTimeout(
		"tcp",
		net.JoinHostPort(targetHost, strconv.Itoa(targetPort)),
		12*time.Second,
	)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))

	if c.Mode == "tls" || c.Mode == "payload_tls" {
		if c.SNI == "" {
			conn.Close()
			return nil, errors.New("SNI belum diisi")
		}
		t := tlsClient(conn, c.SNI)
		if err = t.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("TLS handshake gagal: %w", err)
		}
		conn = t
	}

	if c.Mode == "payload" || c.Mode == "payload_tls" {
		if strings.TrimSpace(c.Payload) == "" {
			conn.Close()
			return nil, errors.New("payload belum diisi")
		}
		if err = writePayload(conn, c.Payload, c); err != nil {
			conn.Close()
			return nil, err
		}
		conn, err = consumeProxyResponse(conn)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// Separate file helper avoids TLS configuration details in the request handlers.
func tlsClient(c net.Conn, serverName string) *tls.Conn { return newTLSClient(c, serverName) }

func connectSSH(c Config) error {
	conn, err := dialTunnel(c)
	if err != nil {
		return err
	}
	var auth []ssh.AuthMethod
	if c.AuthMethod == "privatekey" {
		key, e := os.ReadFile(c.SSHPrivateKey)
		if e != nil {
			conn.Close()
			return e
		}
		signer, e := ssh.ParsePrivateKey(key)
		if e != nil {
			conn.Close()
			return e
		}
		auth = append(auth, ssh.PublicKeys(signer))
	} else {
		auth = append(auth, ssh.Password(c.SSHPassword))
	}
	clientCfg := &ssh.ClientConfig{User: c.SSHUser, Auth: auth, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 20 * time.Second}
	cc, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(c.SSHHost, strconv.Itoa(c.SSHPort)), clientCfg)
	if err != nil {
		conn.Close()
		return err
	}
	client := ssh.NewClient(cc, chans, reqs)
	ln, err := net.Listen("tcp", net.JoinHostPort(c.SOCKSHost, strconv.Itoa(c.SOCKSPort)))
	if err != nil {
		client.Close()
		return err
	}
	mu.Lock()
	sshClient = client
	socksListener = ln
	running = true
	mu.Unlock()
	logf("SSH tersambung; SOCKS5 aktif di %s:%d", c.SOCKSHost, c.SOCKSPort)
	if err := proxyControl("start"); err != nil {
		logf("Peringatan: full-device routing gagal: %v", err)
	}
	go acceptSOCKS(ln, client)
	return nil
}
func stopTunnel() {
	mu.Lock()
	cl, ln := sshClient, socksListener
	sshClient = nil
	socksListener = nil
	running = false
	mu.Unlock()
	if ln != nil {
		ln.Close()
	}
	if cl != nil {
		cl.Close()
	}
	if err := proxyControl("stop"); err != nil {
		logf("Peringatan cleanup proxy: %v", err)
	}
	logf("Tunnel dihentikan")
}
func startTunnel() error {
	mu.Lock()
	if running {
		mu.Unlock()
		return errors.New("tunnel sudah berjalan")
	}
	c := cfg
	mu.Unlock()
	if c.SSHHost == "" || c.SSHUser == "" {
		return errors.New("SSH host dan username wajib diisi")
	}
	err := connectSSH(c)
	if err != nil {
		logf("Gagal koneksi: %v", err)
	}
	return err
}
func acceptSOCKS(ln net.Listener, cl *ssh.Client) {
	for {
		c, e := ln.Accept()
		if e != nil {
			return
		}
		go handleSOCKS(c, cl)
	}
}
func handleSOCKS(c net.Conn, cl *ssh.Client) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(30 * time.Second))
	h := make([]byte, 2)
	if _, e := io.ReadFull(c, h); e != nil {
		return
	}
	methods := make([]byte, int(h[1]))
	if _, e := io.ReadFull(c, methods); e != nil {
		return
	}
	c.Write([]byte{5, 0})
	req := make([]byte, 4)
	if _, e := io.ReadFull(c, req); e != nil {
		return
	}
	if req[0] != 5 || req[1] != 1 {
		c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	var host string
	switch req[3] {
	case 1:
		b := make([]byte, 4)
		if _, e := io.ReadFull(c, b); e != nil {
			return
		}
		host = net.IP(b).String()
	case 3:
		b := make([]byte, 1)
		if _, e := io.ReadFull(c, b); e != nil {
			return
		}
		d := make([]byte, int(b[0]))
		if _, e := io.ReadFull(c, d); e != nil {
			return
		}
		host = string(d)
	case 4:
		b := make([]byte, 16)
		if _, e := io.ReadFull(c, b); e != nil {
			return
		}
		host = net.IP(b).String()
	default:
		return
	}
	pb := make([]byte, 2)
	if _, e := io.ReadFull(c, pb); e != nil {
		return
	}
	port := int(pb[0])<<8 | int(pb[1])
	up, e := cl.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if e != nil {
		c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	c.SetDeadline(time.Time{})
	done := make(chan struct{}, 2)
	go func() { io.Copy(up, c); done <- struct{}{} }()
	go func() { io.Copy(c, up); done <- struct{}{} }()
	<-done
	up.Close()
}
func main() {
	loadConfig()
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(filepath.Join(moduleRoot(), "web"))))
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			mu.Lock()
			defer mu.Unlock()
			json.NewEncoder(w).Encode(cfg)
			return
		}
		if r.Method == "POST" {
			var c Config
			if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&c); e != nil {
				http.Error(w, e.Error(), 400)
				return
			}
			if e := saveConfig(c); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			mu.Lock()
			cfg = c
			mu.Unlock()
			w.Write([]byte(`{"ok":true}`))
			return
		}
		http.Error(w, "method not allowed", 405)
	})
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewEncoder(w).Encode(logLines)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"running": running, "socks": net.JoinHostPort(cfg.SOCKSHost, strconv.Itoa(cfg.SOCKSPort))})
	})
	mux.HandleFunc("/api/action/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		a := strings.TrimPrefix(r.URL.Path, "/api/action/")
		var e error
		switch a {
		case "start":
			e = startTunnel()
		case "stop":
			stopTunnel()
		case "restart":
			stopTunnel()
			time.Sleep(300 * time.Millisecond)
			e = startTunnel()
		default:
			http.Error(w, "unknown action", 404)
			return
		}
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})
	addr := net.JoinHostPort(cfg.WebHost, strconv.Itoa(cfg.WebPort))
	logf("Panel web berjalan di http://%s", addr)
	if e := http.ListenAndServe(addr, mux); e != nil {
		log.Fatal(e)
	}
}
