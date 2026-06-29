package validator

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	govalidator "github.com/go-playground/validator/v10"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func Bind(c *gin.Context, target any) error {
	if err := c.ShouldBindJSON(target); err != nil {
		return formatValidationError(err)
	}

	return nil
}

func formatValidationError(err error) error {
	var details []FieldError

	if validationErrors, ok := err.(govalidator.ValidationErrors); ok {
		for _, fieldError := range validationErrors {
			details = append(details, FieldError{
				Field:   jsonFieldName(fieldError.Field()),
				Message: messageForTag(fieldError),
			})
		}

		return coreerrors.Validation("Erreur de validation", details)
	}

	return coreerrors.Validation("Données invalides", err.Error())
}

func jsonFieldName(field string) string {
	if field == "" {
		return field
	}

	return strings.ToLower(field[:1]) + field[1:]
}

func messageForTag(err govalidator.FieldError) string {
	field := jsonFieldName(err.Field())

	switch err.Tag() {
	case "required":
		return fmt.Sprintf("Le champ %s est obligatoire.", field)

	case "email":
		return fmt.Sprintf("Le champ %s doit être une adresse email valide.", field)

	case "min":
		return fmt.Sprintf("Le champ %s doit contenir au minimum %s caractères.", field, err.Param())

	case "max":
		return fmt.Sprintf("Le champ %s doit contenir au maximum %s caractères.", field, err.Param())

	case "oneof":
		return fmt.Sprintf("Le champ %s doit avoir une valeur autorisée : %s.", field, err.Param())

	case "gte":
		return fmt.Sprintf("Le champ %s doit être supérieur ou égal à %s.", field, err.Param())

	case "lte":
		return fmt.Sprintf("Le champ %s doit être inférieur ou égal à %s.", field, err.Param())

	default:
		return fmt.Sprintf("Le champ %s est invalide.", field)
	}
}
