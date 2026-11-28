package plugin

import (
	"errors"

	"github.com/odango-chan/ca-openamsm-plugin/internal/event"
	"github.com/odango-chan/ca-openamsm-plugin/internal/sbi"
	"github.com/odango-chan/ca-openamsm-plugin/internal/staging"
)

func asSBIError(err error, target **sbi.Error) bool { return errors.As(err, target) }

func asEventError(err error, target **event.Error) bool { return errors.As(err, target) }

func asStagingError(err error, target **staging.Error) bool { return errors.As(err, target) }
