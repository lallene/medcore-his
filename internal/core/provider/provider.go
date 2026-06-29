package provider

import "github.com/lallene/medcore-his/backend/internal/core/application"

type Provider interface {
	Register(app *application.Application)
}
