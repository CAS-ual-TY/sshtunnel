package main

import (
	"flag"
	"github.com/cas-ual-ty/sshtunnel/golang/sshtunnel"
	"golang.org/x/crypto/ssh"
	"log"
	"time"
)

var (
	host       = flag.String("sshhost", "207.180.199.119", "SSH server host (address)")
	port       = flag.String("sshport", "22", "SSH server port")
	user       = flag.String("sshuser", "root", "SSH server user")
	password   = flag.String("sshpassword", "HMst9uSY6QKPSbad8bLzrfdF80vqjl", "SSH server password")
	localPort  = flag.String("lport", "8080", "Local port of SSH tunnel")
	remotePort = flag.String("rport", "8080", "Remote port of SSH tunnel")
	server     = flag.Bool("server", false, "Server (true) or client (false)")
)

func main() {
	flag.Parse()

	sshConfig := &ssh.ClientConfig{
		User: *user,
		Auth: []ssh.AuthMethod{
			ssh.Password(*password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshAddress := *host + ":" + *port

	log.Println("Starting SSH connection...")
	sshConn := sshtunnel.NewSSHTunnel(sshConfig, sshAddress, *localPort, *remotePort)

	if *server {
		sshConn.ReverseTunnel()
	} else {
		sshConn.ForwardTunnel()
	}

	time.Sleep(time.Minute * 2)

	sshConn.Close()
}
