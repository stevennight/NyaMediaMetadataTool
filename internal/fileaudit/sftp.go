package fileaudit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type SFTPConfig struct {
	Addr                  string
	User                  string
	Password              string
	KeyPath               string
	KnownHostsPath        string
	InsecureIgnoreHostKey bool
	Timeout               time.Duration
}

type SFTPFS struct {
	client *sftp.Client
	ssh    *ssh.Client
}

func NewSFTPFS(ctx context.Context, cfg SFTPConfig) (*SFTPFS, error) {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		return nil, errors.New("sftp addr is required")
	}
	if !strings.Contains(addr, ":") {
		addr += ":22"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	sshConfig, err := sshClientConfig(cfg)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		conn.Close()
		return nil, err
	}
	sshClient := ssh.NewClient(sshConn, chans, reqs)
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, err
	}
	return &SFTPFS{client: sftpClient, ssh: sshClient}, nil
}

func (fs *SFTPFS) Close() error {
	var err error
	if fs.client != nil {
		err = fs.client.Close()
	}
	if fs.ssh != nil {
		if closeErr := fs.ssh.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func (fs *SFTPFS) Walk(ctx context.Context, root string, fn func(FileInfo) error) error {
	walker := fs.client.Walk(cleanRemoteRoot(root))
	for walker.Step() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := walker.Err(); err != nil {
			return err
		}
		info := walker.Stat()
		if info == nil || info.IsDir() {
			continue
		}
		if err := fn(FileInfo{Path: cleanRemoteRoot(walker.Path()), Size: info.Size()}); err != nil {
			return err
		}
	}
	return nil
}

func (fs *SFTPFS) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return fs.client.Open(cleanRemoteRoot(name))
}

func sshClientConfig(cfg SFTPConfig) (*ssh.ClientConfig, error) {
	auth, err := sshAuthMethods(cfg)
	if err != nil {
		return nil, err
	}
	hostKeyCallback, err := sshHostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{User: strings.TrimSpace(cfg.User), Auth: auth, HostKeyCallback: hostKeyCallback, Timeout: cfg.Timeout}, nil
}

func sshAuthMethods(cfg SFTPConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if strings.TrimSpace(cfg.Password) != "" {
		methods = append(methods, ssh.Password(cfg.Password))
		methods = append(methods, ssh.KeyboardInteractive(func(user string, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = cfg.Password
			}
			return answers, nil
		}))
	}
	if strings.TrimSpace(cfg.KeyPath) != "" {
		key, err := os.ReadFile(cfg.KeyPath)
		if err != nil {
			return nil, err
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, err
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if len(methods) == 0 {
		return nil, errors.New("sftp password or key path is required")
	}
	return methods, nil
}

func sshHostKeyCallback(cfg SFTPConfig) (ssh.HostKeyCallback, error) {
	if strings.TrimSpace(cfg.KnownHostsPath) != "" {
		return knownhosts.New(cfg.KnownHostsPath)
	}
	if cfg.InsecureIgnoreHostKey {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	return nil, fmt.Errorf("sftp known_hosts path is required unless insecure host key checking is enabled")
}

func JoinRemote(root string, rel string) string {
	return path.Join(cleanRemoteRoot(root), filepathToSlash(rel))
}

func filepathToSlash(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}
