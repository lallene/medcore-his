package auth

import (
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"github.com/lallene/medcore-his/backend/internal/core/validator"
)

type Handler struct {
	db        *gorm.DB
	jwtSecret string
}

func NewHandler(db *gorm.DB, jwtSecret string) *Handler {
	return &Handler{
		db:        db,
		jwtSecret: jwtSecret,
	}
}

func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	var user User

	if err := h.db.Where("email = ? AND is_active = ?", req.Email, true).First(&user).Error; err != nil {
		response.Error(c, coreerrors.Unauthorized("Identifiants invalides"))
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		response.Error(c, coreerrors.Unauthorized("Identifiants invalides"))
		return
	}

	permissions := permissionsForRole(user.Role)

	token, err := GenerateToken(h.jwtSecret, user, permissions)

	if err != nil {
		response.Error(c, coreerrors.Internal("Erreur génération token"))
		return
	}

	response.Success(c, "Connexion réussie", LoginResponse{
		Token: token,
		User: UserResponse{
			ID:    user.ID,
			Name:  user.Name,
			Email: user.Email,
			Role:  user.Role,
		},
	})
}

func permissionsForRole(role string) []string {
	if role == "admin" {
		return []string{"*"}
	}

	if role == "accueil" {
		return []string{
			"patients:read",
			"patients:create",
			"patients:update",
		}
	}

	return []string{}
}
