package email

import (
	"bytes"
	"fmt"
	"html/template"
	"path"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/gomail.v2"
)

type EmailOptions struct {
	// The name of the Club, Society, or Project the website relates to
	CSP string
	// The email address to send to
	Email string
	// The email name of the recipient (i.e. shown alongside the email address in the From field)
	EmailName string
	// The first name of the recipient
	FirstName string
	// The website folder (same as the site name)
	Folder string
	// Subject of the email
	Subject string
	// The type of email to send. Should be one of "granted", "revoked", or "test"
	Type string
}

type templateData struct {
	Name   string
	CSP    string
	Folder string
}

type workerStruct struct {
	msgChan chan *gomail.Message
	wg      sync.WaitGroup
	started bool
}

var worker workerStruct

var allowedTypes = map[string]bool{
	"granted": true,
	"revoked": true,
	"test":    true,
}

func init() {
	viper.SetDefault("email.host", "localhost")
	viper.SetDefault("email.port", 25)
	viper.SetDefault("email.resources_path", "~/pugo/res")
	viper.SetDefault("email.sender.name", "pugo")
	viper.SetDefault("email.sender.email", "pugo@example.com")

	worker = workerStruct{
		msgChan: make(chan *gomail.Message, 5),
	}
}

func StartWorker() error {
	log.Debug("email: Starting send worker ...")
	if worker.started {
		log.Debug("email: Send worker already running")
		return nil
	}

	d := &gomail.Dialer{
		Host: viper.GetString("email.host"),
		Port: viper.GetInt("email.port"),
	}
	if smtpUsername := viper.GetString("email.username"); smtpUsername != "" {
		d.Username = smtpUsername
		d.Password = viper.GetString("email.password")
	}

	if s, err := d.Dial(); err != nil {
		return fmt.Errorf("email: Error dialing smtp: %v", err)
	} else {
		s.Close()
	}

	worker.started = true
	worker.wg.Add(1)
	go func(d *gomail.Dialer) {
		var s gomail.SendCloser
		var err error
		open := false

		log.Info("email: Send worker started")
		for {
			select {
			case msg, ok := <-worker.msgChan:
				if !ok {
					log.Info("email: Send worker stopped")
					worker.started = false
					worker.wg.Done()
					return
				}
				if !open {
					if s, err = d.Dial(); err != nil {
						log.Warnf("email: Sending to %s: Error dialing smtp: %v", msg.GetHeader("To")[0], err)
						break
					}
					open = true
				}
				log.Infof("email: Sending to %s", msg.GetHeader("To")[0])
				if err := gomail.Send(s, msg); err != nil {
					log.Warnf("email: Sending to %s: Error sending message: %v", msg.GetHeader("To")[0], err)
				}
			// In the unlikely event we're running for a long
			// time and no email is sent for more than 10
			// seconds, close the connection
			case <-time.After(10 * time.Second):
				if open {
					if err := s.Close(); err != nil {
						log.Warnf("email: Error closing smtp: %v", err)
						break
					}
					open = false
				}
			}
		}
	}(d)

	return nil
}

func ShutdownWorker() {
	close(worker.msgChan)
	worker.wg.Wait()
}

func SendEmail(opts *EmailOptions) error {
	if !allowedTypes[opts.Type] {
		return fmt.Errorf("email: Unknown message type %s", opts.Type)
	}

	msg := gomail.NewMessage()
	msg.SetAddressHeader("From", viper.GetString("email.sender.email"), viper.GetString("email.sender.name"))
	msg.SetAddressHeader("To", opts.Email, opts.EmailName)
	msg.SetHeader("Subject", opts.Subject)
	msg.Embed(resourcePath("img", "sysheader.jpg"))
	msg.Embed(resourcePath("img", "sysfooter.jpg"))

	tpl, err := template.ParseFiles(resourcePath("tpl", "email-layout.gohtml"), resourcePath("tpl", "email-"+opts.Type+".gohtml"))
	if err != nil {
		return fmt.Errorf("email: Parsing templates layout, %s: %v", opts.Type, err)
	}

	bodyBuff := new(bytes.Buffer)

	data := templateData{
		Name:   opts.FirstName,
		CSP:    opts.CSP,
		Folder: opts.Folder,
	}

	if err := tpl.ExecuteTemplate(bodyBuff, opts.Type, data); err != nil {
		return fmt.Errorf("email: Executing templates layout, %s: %v", opts.Type, err)
	}

	msg.SetBody("text/html", bodyBuff.String())

	worker.msgChan <- msg

	return nil
}

func resourcePath(elements ...string) string {
	elements = append([]string{viper.GetString("email.resources_path")}, elements...)
	return path.Join(elements...)
}
