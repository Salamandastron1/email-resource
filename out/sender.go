package out

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"os"
	"path/filepath"

	"github.com/domodwyer/mailyak"
	"github.com/pkg/errors"
)

func NewSender(host, port string, debug bool, logger *log.Logger) *Sender {
	return &Sender{
		host:        host,
		port:        port,
		attachments: make(map[string]io.Reader),
		debug:       debug,
		logger:      logger,
	}
}

type Sender struct {
	host                                    string
	port                                    string
	attachments                             map[string]io.Reader
	debug                                   bool
	logger                                  *log.Logger
	HostOrigin                              string
	CaCert                                  string
	Anonymous, LoginAuth, SkipSSLValidation bool
	Username                                string
	Password                                string
	From                                    string
	To, Cc, Bcc                             []string
	Subject                                 string
	Body                                    string
	Headers                                 map[string]string
}

func (s *Sender) AddAttachment(filePath string) error {
	reader, err := os.Open(filePath)
	if err != nil {
		return err
	}
	s.attachments[filepath.Base(reader.Name())] = reader
	return nil
}

func (s *Sender) Send() error {
	msg := mailyak.New("", nil)
	msg.From(s.From)
	msg.To(s.To...)
	msg.Cc(s.Cc...)
	msg.Bcc(s.Bcc...)
	msg.Subject(s.Subject)
	if s.Headers != nil {
		for key, value := range s.Headers {
			msg.AddHeader(key, value)
		}
	}
	for name, reader := range s.attachments {
		msg.Attach(name, reader)
	}
	msg.Plain().WriteString(s.Body)
	buf, err := msg.MimeBuf()
	if err != nil {
		return errors.Wrap(err, "unable to get mime buffer")
	}

	var c *smtp.Client
	var wc io.WriteCloser
	if s.debug {
		s.logger.Println("Dialing")
	}
	c, err = smtp.Dial(fmt.Sprintf("%s:%s", s.host, s.port))
	if err != nil {
		return errors.Wrap(err, "Error Dialing smtp server")
	}
	defer c.Close()

	hostOrigin := "localhost"

	if s.HostOrigin != "" {
		hostOrigin = s.HostOrigin
	}
	if s.debug {
		s.logger.Println("Saying Hello to SMTP Server")
	}
	if err = c.Hello(hostOrigin); err != nil {
		return errors.Wrap(err, fmt.Sprintf("unable to connect with hello with host name %s, try setting property host_origin", hostOrigin))
	}
	if s.debug {
		s.logger.Println("STARTTLS with SMTP Server")
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		config := s.tlsConfig()

		if err = c.StartTLS(config); err != nil {
			return errors.Wrap(err, "unable to start TLS")
		}
	}

	if s.debug {
		s.logger.Println("Authenticating with SMTP Server")
	}
	err = s.doAuth(c)
	if err != nil {
		return errors.Wrap(err, "Error doing auth:")
	}
	if s.debug {
		s.logger.Println("Setting From")
	}
	if err = c.Mail(s.From); err != nil {
		return errors.Wrap(err, "Error setting from:")
	}
	if s.debug {
		s.logger.Println("Setting TO")
	}
	to := append(append(s.To, s.Cc...), s.Bcc...)
	for _, addr := range to {
		if err = c.Rcpt(addr); err != nil {
			return errors.Wrap(err, "Error setting to:")
		}
	}

	if s.debug {
		s.logger.Println("Getting Data from SMTP Server")
	}
	wc, err = c.Data()
	if err != nil {
		return errors.Wrap(err, "Error getting Data:")
	}
	if s.debug {
		s.logger.Println(fmt.Sprintf("Writing message to SMTP Server %s", string(buf.Bytes())))
	}
	_, err = wc.Write(buf.Bytes())
	if err != nil {
		return errors.Wrap(err, "Error writting message data:")
	}
	if s.debug {
		s.logger.Println("Closing connection to SMTP Server")
	}
	err = wc.Close()
	if err != nil {
		return errors.Wrap(err, "Error closing:")
	}
	if s.debug {
		s.logger.Println("Quitting connection to SMTP Server")
	}
	err = c.Quit()
	if err != nil {
		return errors.Wrap(err, "Error quitting:")
	}
	return nil
}

func (s *Sender) tlsConfig() *tls.Config {
	config := &tls.Config{
		ServerName: s.host,
	}

	if s.SkipSSLValidation {
		config.InsecureSkipVerify = s.SkipSSLValidation
		return config
	}

	if s.CaCert != "" {
		caPool := x509.NewCertPool()
		caPool.AppendCertsFromPEM([]byte(s.CaCert))
		config.RootCAs = caPool
		return config
	}

	return config
}

func (s *Sender) doAuth(c *smtp.Client) error {
	if s.Anonymous {
		return nil
	}
	if s.LoginAuth {
		auth := LoginAuth(s.Username, s.Password)

		if auth != nil {
			if ok, _ := c.Extension("AUTH"); ok {
				if err := c.Auth(auth); err != nil {
					return errors.Wrap(err, "unable to auth using type Login Auth")
				}
			}
		}
	} else {
		auth := smtp.PlainAuth(
			"",
			s.Username,
			s.Password,
			s.host,
		)
		if auth != nil {
			if ok, _ := c.Extension("AUTH"); ok {
				if err := c.Auth(auth); err != nil {
					return errors.Wrap(err, "unable to auth using type Plain Auth")
				}
			}
		}
	}
	return nil
}
