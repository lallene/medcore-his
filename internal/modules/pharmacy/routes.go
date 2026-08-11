package pharmacy

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutesWithHandler(router *gin.RouterGroup, handler *Handler) {
	pharmacy := router.Group("/pharmacy")
	{
		pharmacy.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"module": "pharmacy",
				"status": "ok",
			})
		})

		pharmacy.GET("/families", handler.GetFamilies)

		pharmacy.POST(
			"/families",
			rbac.Permission("pharmacy.references.manage"),
			handler.CreateFamily,
		)

		pharmacy.PUT(
			"/families/:id",
			rbac.Permission("pharmacy.references.manage"),
			handler.UpdateFamily,
		)

		pharmacy.DELETE(
			"/families/:id",
			rbac.Permission("pharmacy.references.manage"),
			handler.DeleteFamily,
		)

		pharmacy.GET("/medications", handler.GetMedications)

		pharmacy.POST(
			"/medications",
			rbac.Permission("pharmacy.references.manage"),
			handler.CreateMedication,
		)

		pharmacy.PUT(
			"/medications/:id",
			rbac.Permission("pharmacy.references.manage"),
			handler.UpdateMedication,
		)

		pharmacy.DELETE(
			"/medications/:id",
			rbac.Permission("pharmacy.references.manage"),
			handler.DeleteMedication,
		)

		pharmacy.GET("/presentations", handler.GetPresentations)
		pharmacy.GET("/presentations/availability", handler.GetPresentationAvailability)

		pharmacy.POST(
			"/presentations",
			rbac.Permission("pharmacy.references.manage"),
			handler.CreatePresentation,
		)

		pharmacy.PUT(
			"/presentations/:id",
			rbac.Permission("pharmacy.references.manage"),
			handler.UpdatePresentation,
		)

		pharmacy.DELETE(
			"/presentations/:id",
			rbac.Permission("pharmacy.references.manage"),
			handler.DeletePresentation,
		)

		pharmacy.GET(
			"/stocks",
			rbac.Permission("pharmacy.stock.read"),
			handler.GetStocks,
		)

		pharmacy.POST(
			"/stocks",
			rbac.Permission("pharmacy.stock.manage"),
			handler.CreateStock,
		)

		pharmacy.PUT(
			"/stocks/:id",
			rbac.Permission("pharmacy.stock.manage"),
			handler.UpdateStock,
		)

		pharmacy.GET(
			"/batches",
			rbac.Permission("pharmacy.stock.read"),
			handler.GetBatches,
		)

		pharmacy.POST(
			"/batches",
			rbac.Permission("pharmacy.stock.manage"),
			handler.CreateBatch,
		)

		pharmacy.GET(
			"/stock-movements",
			rbac.Permission("pharmacy.stock.read"),
			handler.GetStockMovements,
		)

		pharmacy.GET(
			"/presentations/:presentationId/stock-movements",
			rbac.Permission("pharmacy.stock.read"),
			handler.GetPresentationStockMovements,
		)

		pharmacy.GET(
			"/dispensations",
			rbac.Permission("pharmacy.dispensation.read"),
			handler.GetDispensations,
		)

		pharmacy.POST(
			"/dispensations",
			rbac.Permission("pharmacy.dispensation.create"),
			handler.CreateDispensation,
		)

		pharmacy.GET(
			"/prescriptions/:id/dispensation-status",
			handler.GetPrescriptionDispensationStatus,
		)

		pharmacy.GET(
			"/prescriptions/pending",
			rbac.Permission("pharmacy.dispensation.read"),
			handler.GetPrescriptionQueue,
		)
		pharmacy.GET("/vouchers", rbac.Permission("pharmacy.dispensation.read"), handler.GetVouchers)
		pharmacy.GET("/vouchers/:id", rbac.Permission("pharmacy.dispensation.read"), handler.GetVoucher)
	}
}
