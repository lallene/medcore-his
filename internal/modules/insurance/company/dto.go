package company

type CreateCompanyRequest struct {
	Code        string `json:"code" binding:"required,min=2,max=50"`
	Name        string `json:"name" binding:"required,min=2,max=150"`
	Description string `json:"description"`
	Phone       string `json:"phone" binding:"omitempty,max=50"`
	Email       string `json:"email" binding:"omitempty,email,max=150"`
	Address     string `json:"address"`
	City        string `json:"city" binding:"omitempty,max=100"`
	Country     string `json:"country" binding:"omitempty,max=100"`
}

type UpdateCompanyRequest struct {
	Code        string `json:"code" binding:"omitempty,min=2,max=50"`
	Name        string `json:"name" binding:"omitempty,min=2,max=150"`
	Description string `json:"description"`
	Phone       string `json:"phone" binding:"omitempty,max=50"`
	Email       string `json:"email" binding:"omitempty,email,max=150"`
	Address     string `json:"address"`
	City        string `json:"city" binding:"omitempty,max=100"`
	Country     string `json:"country" binding:"omitempty,max=100"`
	IsActive    *bool  `json:"isActive"`
}

type CompanyResponse struct {
	ID          uint   `json:"id"`
	UUID        string `json:"uuid"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Address     string `json:"address"`
	City        string `json:"city"`
	Country     string `json:"country"`
	IsActive    bool   `json:"isActive"`
}

type CompanySummary struct {
	ID       uint   `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsActive bool   `json:"isActive"`
}
