package hospitalizations

type CreateRoomRequest struct {
	Code       string `json:"code" binding:"required"`
	Name       string `json:"name" binding:"required"`
	Department string `json:"department" binding:"required"`
	Floor      string `json:"floor"`
	RoomType   string `json:"roomType" binding:"required"`
	IsActive   *bool  `json:"isActive"`
}
type UpdateRoomRequest struct {
	Code       *string `json:"code"`
	Name       *string `json:"name"`
	Department *string `json:"department"`
	Floor      *string `json:"floor"`
	RoomType   *string `json:"roomType"`
	IsActive   *bool   `json:"isActive"`
}

type CreateBedRequest struct {
	RoomID   uint   `json:"roomId" binding:"required"`
	Code     string `json:"code" binding:"required"`
	Label    string `json:"label" binding:"required"`
	BedType  string `json:"bedType" binding:"required"`
	IsActive *bool  `json:"isActive"`
}
type UpdateBedRequest struct {
	Code     *string `json:"code"`
	RoomID   *uint   `json:"roomId"`
	Label    *string `json:"label"`
	BedType  *string `json:"bedType"`
	Status   *string `json:"status"`
	IsActive *bool   `json:"isActive"`
}
type AssignBedRequest struct {
	BedID uint `json:"bedId" binding:"required"`
}
type TransferBedRequest struct {
	BedID uint `json:"bedId" binding:"required"`
}

type BedFilter struct {
	Page, Limit        int
	Department, Status string
	RoomID             *uint
	Active, Available  *bool
}
type BedListResult struct {
	Data        []BedOverview
	Page, Limit int
	Total       int64
	TotalPages  int
}
type BedOverview struct {
	Bed              Bed            `json:"bed"`
	ActiveAssignment *BedAssignment `json:"activeAssignment"`
}
