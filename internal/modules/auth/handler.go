package auth

import (
	"github.com/gin-gonic/gin"
	_ "github.com/lallene/medcore-his/backend/internal/core/openapi"
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

// Login godoc
//
//	@Summary		Connexion utilisateur
//	@Description	Authentifie un utilisateur MedCore et retourne un JWT.
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Param			request	body		LoginRequest	true	"Identifiants utilisateur"
//	@Success		200		{object}	openapi.SuccessResponse
//	@Failure		400		{object}	openapi.ErrorResponse
//	@Failure		401		{object}	openapi.ErrorResponse
//	@Router			/auth/login [post]
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
			"hospitalizations.read",
			"rooms.read",
			"beds.read",
			"bed_assignments.read",
		}
	}

	return []string{}
}
