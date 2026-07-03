package consultations

import "gorm.io/gorm"

func SeedConsultationReferences(db *gorm.DB) {
	reasons := []ConsultationReason{
		{Code: "FEVER", Name: "Fièvre", Category: "Symptôme"},
		{Code: "HEADACHE", Name: "Céphalées", Category: "Symptôme"},
		{Code: "MUSCLE_PAIN", Name: "Douleurs musculaires", Category: "Symptôme"},
		{Code: "MALARIA", Name: "Paludisme", Category: "Pathologie"},
		{Code: "COUGH", Name: "Toux", Category: "Symptôme"},
		{Code: "ABDOMINAL_PAIN", Name: "Douleurs abdominales", Category: "Symptôme"},
	}

	for _, reason := range reasons {
		db.FirstOrCreate(&reason, ConsultationReason{Code: reason.Code})
	}

	exams := []MedicalExam{
		{Code: "NFS", Name: "NFS", Category: "Biologie"},
		{Code: "GLYCEMIA", Name: "Glycémie", Category: "Biologie"},
		{Code: "CRP", Name: "CRP", Category: "Biologie"},
		{Code: "THICK_DROP", Name: "Goutte épaisse", Category: "Biologie"},
		{Code: "CHEST_XRAY", Name: "Radiographie thorax", Category: "Imagerie"},
		{Code: "ABDOMINAL_ULTRASOUND", Name: "Échographie abdominale", Category: "Imagerie"},
		{Code: "CT_SCAN", Name: "Scanner", Category: "Imagerie"},
		{Code: "MRI", Name: "IRM", Category: "Imagerie"},
		{Code: "ECG", Name: "ECG", Category: "Cardiologie"},
	}

	for _, exam := range exams {
		db.FirstOrCreate(&exam, MedicalExam{Code: exam.Code})
	}
}
