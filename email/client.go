package email

import "net/smtp"

var Client *ClientWrapper

type ClientWrapper struct {
	auth smtp.Auth
}

type TemplateConfig interface {
	TemplateMaxImageWidth() int
	TemplateMaxImageHeight() int
}

type Config interface {
	EmailHost() string
	EmailPort() int
	EmailFrom() string
	EmailTo() []string
	EmailPassword() string
	EmailTemplateConfigFor(path TemplatePath) TemplateConfig
}

func ClientCreate(config Config) {
	Client = &ClientWrapper{
		auth: smtp.PlainAuth("", config.EmailFrom(), config.EmailPassword(), config.EmailHost()),
	}
}
