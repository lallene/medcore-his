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

func SeedPhysicalExamAreas(db *gorm.DB) error {
	areas := []PhysicalExamArea{
		// État général
		{Code: "GENERAL_STATE", Category: "État général", Name: "État général", IsActive: true},
		{Code: "CONSCIOUSNESS", Category: "État général", Name: "Conscience", IsActive: true},
		{Code: "HYDRATION", Category: "État général", Name: "Hydratation", IsActive: true},
		{Code: "NUTRITIONAL_STATE", Category: "État général", Name: "État nutritionnel", IsActive: true},
		{Code: "LYMPH_NODES", Category: "État général", Name: "Aires ganglionnaires", IsActive: true},

		// Cardiovasculaire
		{Code: "HEART", Category: "Appareil cardiovasculaire", Name: "Cœur", IsActive: true},
		{Code: "PERIPHERAL_PULSES", Category: "Appareil cardiovasculaire", Name: "Pouls périphériques", IsActive: true},
		{Code: "PERIPHERAL_CIRCULATION", Category: "Appareil cardiovasculaire", Name: "Circulation périphérique", IsActive: true},

		// Respiratoire
		{Code: "THORAX", Category: "Appareil respiratoire", Name: "Thorax", IsActive: true},
		{Code: "LUNGS", Category: "Appareil respiratoire", Name: "Poumons", IsActive: true},

		// Digestif
		{Code: "ABDOMEN", Category: "Appareil digestif", Name: "Abdomen", IsActive: true},
		{Code: "LIVER", Category: "Appareil digestif", Name: "Foie", IsActive: true},
		{Code: "SPLEEN", Category: "Appareil digestif", Name: "Rate", IsActive: true},
		{Code: "RECTAL", Category: "Appareil digestif", Name: "Région ano-rectale", IsActive: true},

		// Neurologique
		{Code: "CRANIAL_NERVES", Category: "Appareil neurologique", Name: "Nerfs crâniens", IsActive: true},
		{Code: "MOTOR_FUNCTION", Category: "Appareil neurologique", Name: "Motricité", IsActive: true},
		{Code: "SENSORY_FUNCTION", Category: "Appareil neurologique", Name: "Sensibilité", IsActive: true},
		{Code: "REFLEXES", Category: "Appareil neurologique", Name: "Réflexes", IsActive: true},
		{Code: "COORDINATION", Category: "Appareil neurologique", Name: "Coordination et équilibre", IsActive: true},

		// Locomoteur
		{Code: "SPINE", Category: "Appareil locomoteur", Name: "Rachis", IsActive: true},
		{Code: "JOINTS", Category: "Appareil locomoteur", Name: "Articulations", IsActive: true},
		{Code: "MUSCLES", Category: "Appareil locomoteur", Name: "Muscles", IsActive: true},
		{Code: "UPPER_LIMBS", Category: "Appareil locomoteur", Name: "Membres supérieurs", IsActive: true},
		{Code: "LOWER_LIMBS", Category: "Appareil locomoteur", Name: "Membres inférieurs", IsActive: true},

		// Génito-urinaire
		{Code: "KIDNEYS", Category: "Appareil génito-urinaire", Name: "Reins", IsActive: true},
		{Code: "BLADDER", Category: "Appareil génito-urinaire", Name: "Vessie", IsActive: true},
		{Code: "MALE_GENITALIA", Category: "Appareil génito-urinaire", Name: "Organes génitaux masculins", IsActive: true},

		// Gynécologie / Obstétrique
		{Code: "BREASTS", Category: "Gynécologie et obstétrique", Name: "Seins", IsActive: true},
		{Code: "UTERUS", Category: "Gynécologie et obstétrique", Name: "Utérus", IsActive: true},
		{Code: "CERVIX", Category: "Gynécologie et obstétrique", Name: "Col utérin", IsActive: true},
		{Code: "OBSTETRIC_EXAM", Category: "Gynécologie et obstétrique", Name: "Examen obstétrical", IsActive: true},

		// ORL
		{Code: "EARS", Category: "ORL", Name: "Oreilles", IsActive: true},
		{Code: "NOSE", Category: "ORL", Name: "Nez et sinus", IsActive: true},
		{Code: "THROAT", Category: "ORL", Name: "Gorge et pharynx", IsActive: true},
		{Code: "NECK", Category: "ORL", Name: "Cou", IsActive: true},

		// Ophtalmologie
		{Code: "EYES", Category: "Ophtalmologie", Name: "Yeux", IsActive: true},
		{Code: "VISUAL_ACUITY", Category: "Ophtalmologie", Name: "Acuité visuelle", IsActive: true},

		// Dermatologie
		{Code: "SKIN", Category: "Dermatologie", Name: "Peau", IsActive: true},
		{Code: "HAIR", Category: "Dermatologie", Name: "Cheveux", IsActive: true},
		{Code: "NAILS", Category: "Dermatologie", Name: "Ongles", IsActive: true},

		// Bucco-dentaire
		{Code: "ORAL_CAVITY", Category: "Bucco-dentaire", Name: "Cavité buccale", IsActive: true},
		{Code: "TEETH", Category: "Bucco-dentaire", Name: "Dents", IsActive: true},

		// Psychiatrie
		{Code: "MENTAL_STATE", Category: "Psychiatrie", Name: "État mental", IsActive: true},
		{Code: "BEHAVIOR", Category: "Psychiatrie", Name: "Comportement", IsActive: true},
	}

	for _, area := range areas {
		if err := db.
			Where("code = ?", area.Code).
			FirstOrCreate(&area).Error; err != nil {
			return err
		}
	}

	return nil
}
