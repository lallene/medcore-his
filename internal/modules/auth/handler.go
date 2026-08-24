package auth

import (
	"github.com/gin-gonic/gin"
	_ "github.com/lallene/medcore-his/backend/internal/core/openapi"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
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

	functions, specialties, capabilities, err := staffIdentity(h.db, user.ID)
	if err != nil {
		response.Error(c, coreerrors.Internal("Erreur chargement profil personnel"))
		return
	}
	permissions := rbac.EffectiveStaffPermissions(user.Role, functions, specialties)

	token, err := GenerateToken(h.jwtSecret, user, permissions, functions, specialties, capabilities)

	if err != nil {
		response.Error(c, coreerrors.Internal("Erreur génération token"))
		return
	}

	response.Success(c, "Connexion réussie", LoginResponse{
		Token: token,
		User: UserResponse{
			ID:           user.ID,
			Name:         user.Name,
			Email:        user.Email,
			Role:         user.Role,
			Functions:    functions,
			Specialties:  specialties,
			Capabilities: capabilities,
		},
	})
}

func staffIdentity(db *gorm.DB, userID uint) ([]string, []string, []string, error) {
	var profileID uint
	if err := db.Table("staff_profiles").Select("id").Where("user_id=? AND active", userID).Scan(&profileID).Error; err != nil || profileID == 0 {
		return []string{}, []string{}, []string{}, err
	}
	load := func(table string) ([]string, error) {
		var values []string
		err := db.Table(table).Where("profile_id=? AND active", profileID).Order("code").Pluck("code", &values).Error
		return values, err
	}
	functions, err := load("staff_functions")
	if err != nil {
		return nil, nil, nil, err
	}
	specialties, err := load("staff_specialties")
	if err != nil {
		return nil, nil, nil, err
	}
	capabilities, err := load("staff_capabilities")
	if err != nil {
		return nil, nil, nil, err
	}
	return functions, specialties, capabilities, nil
}
