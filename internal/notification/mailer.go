package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"html/template"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
	AppURL   string
}

func (c SMTPConfig) Enabled() bool {
	return strings.TrimSpace(c.Host) != "" && c.Port > 0 && strings.TrimSpace(c.Username) != "" && strings.TrimSpace(c.Password) != "" && strings.TrimSpace(c.From) != ""
}

type Mailer interface {
	Send(context.Context, Message) error
}

type Message struct {
	RecipientName  string
	RecipientEmail string
	ProjectName    string
	Subject        string
	Summary        string
	ActionPath     string
}

type SMTPMailer struct{ config SMTPConfig }

func NewSMTPMailer(config SMTPConfig) *SMTPMailer { return &SMTPMailer{config: config} }

func (m *SMTPMailer) Send(ctx context.Context, message Message) error {
	if m == nil || !m.config.Enabled() {
		return ErrDeliveryDisabled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	from := fmt.Sprintf("%s <%s>", mimeHeader(m.config.FromName), m.config.From)
	htmlBody, err := renderEmail(emailView{
		RecipientName: firstName(message.RecipientName), ProjectName: message.ProjectName,
		Summary: message.Summary, ActionURL: absoluteURL(m.config.AppURL, message.ActionPath),
	})
	if err != nil {
		return err
	}
	boundary := "millena-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	payload := strings.Join([]string{
		"From: " + from,
		"To: " + message.RecipientEmail,
		"Subject: " + mimeHeader(message.Subject),
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"",
		"",
		"--" + boundary,
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		plainText(message, absoluteURL(m.config.AppURL, message.ActionPath)),
		"--" + boundary,
		"Content-Type: text/html; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		htmlBody,
		"--" + boundary + "--",
		"",
	}, "\r\n")

	address := net.JoinHostPort(m.config.Host, strconv.Itoa(m.config.Port))
	dialer := &net.Dialer{}
	var connection net.Conn
	if m.config.Port == 465 {
		connection, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: m.config.Host, MinVersion: tls.VersionTLS12})
	} else {
		connection, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return err
	}
	defer connection.Close()
	client, err := smtp.NewClient(connection, m.config.Host)
	if err != nil {
		return err
	}
	defer client.Quit()
	if m.config.Port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: m.config.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		}
	}
	if auth := smtp.PlainAuth("", m.config.Username, m.config.Password, m.config.Host); auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(m.config.From); err != nil {
		return err
	}
	if err := client.Rcpt(message.RecipientEmail); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write([]byte(payload)); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func mimeHeader(value string) string {
	return "=?UTF-8?B?" + encodeBase64(strings.TrimSpace(value)) + "?="
}

func encodeBase64(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }

func plainText(message Message, actionURL string) string {
	return fmt.Sprintf("Millena AI\n\n%s\n\n%s\n\nOtvori projekt: %s\n\nOvu obavijest primate jer ste član projekta %s.", message.Subject, message.Summary, actionURL, message.ProjectName)
}

func firstName(value string) string {
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func absoluteURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

type emailView struct{ RecipientName, ProjectName, Summary, ActionURL string }

var emailTemplate = template.Must(template.New("email").Parse(`<!doctype html><html lang="hr"><body style="margin:0;background:#f4f1fb;font-family:Inter,Arial,sans-serif;color:#17151b"><table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="padding:36px 16px"><tr><td align="center"><table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:600px;background:#fff;border-radius:18px;overflow:hidden;box-shadow:0 14px 42px rgba(49,28,79,.12)"><tr><td style="padding:26px 34px;background:linear-gradient(135deg,#281443,#6e3ad0);color:#fff"><div style="font-size:13px;font-weight:800;letter-spacing:1.4px;text-transform:uppercase">Millena AI</div><div style="margin-top:12px;font-size:25px;font-weight:800">Vaš projekt je u toku.</div></td></tr><tr><td style="padding:34px"><p style="margin:0 0 12px;font-size:17px">{{if .RecipientName}}Bok, {{.RecipientName}}!{{else}}Bok!{{end}}</p><p style="margin:0;color:#4c4754;font-size:16px;line-height:1.65">{{.Summary}}</p><table role="presentation" cellpadding="0" cellspacing="0" style="margin:28px 0"><tr><td style="background:#6e3ad0;border-radius:9px"><a href="{{.ActionURL}}" style="display:inline-block;padding:13px 20px;color:#fff;text-decoration:none;font-size:14px;font-weight:800">Otvori u Millena AI →</a></td></tr></table><div style="padding-top:18px;border-top:1px solid #eee8f5;color:#817b8b;font-size:12px;line-height:1.5">Projekt: {{.ProjectName}}<br>Ovu obavijest primate jer ste aktivni član projekta.</div></td></tr></table></td></tr></table></body></html>`))

func renderEmail(data emailView) (string, error) {
	var output bytes.Buffer
	if err := emailTemplate.Execute(&output, data); err != nil {
		return "", err
	}
	return output.String(), nil
}
