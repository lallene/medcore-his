package branding

type RGB struct {
	R int
	G int
	B int
}

type ClinicBranding struct {
	Name      string
	LegalName string
	Tagline   string
	Signature string
	Address   string
	RCCM      string
	LegalForm string
	Capital   string
	Primary   RGB
	Secondary RGB
	Accent    RGB
	Text      RGB
	Muted     RGB
	Border    RGB
	LogoPath  string
}

var Clinic = ClinicBranding{
	Name:      "Clinique Médicale Saint Raphaël Archange",
	LegalName: "Clinique Médicale Saint Raphaël Archange",
	Tagline:   "Excellence - Compassion - Santé",
	Signature: "Humanité et expertise au service de la vie",

	Address: "Séguéla, quartier Résidentiel, 100 m avant l'Église Foursquare, lot 1738, îlot 191",

	RCCM:      "CI-SEG-01-2026-B13-00024",
	LegalForm: "SARLU",
	Capital:   "10 000 000 F CFA",

	Primary: RGB{
		R: 14,
		G: 76,
		B: 146,
	},
	Secondary: RGB{
		R: 18,
		G: 126,
		B: 188,
	},
	Accent: RGB{
		R: 35,
		G: 181,
		B: 156,
	},
	Text: RGB{
		R: 15,
		G: 23,
		B: 42,
	},
	Muted: RGB{
		R: 100,
		G: 116,
		B: 139,
	},
	Border: RGB{
		R: 226,
		G: 232,
		B: 240,
	},

	LogoPath: "internal/core/branding/assets/logo.jpg",
}
