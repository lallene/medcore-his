package patients

import coremapper "github.com/lallene/medcore-his/backend/internal/core/mapper"

func ToResponse(patient *Patient) PatientResponse {
	if patient == nil {
		return PatientResponse{}
	}

	return PatientResponse{
		ID:   patient.ID,
		UUID: patient.UUID,

		CodePatient:   patient.CodePatient,
		NumeroDossier: patient.NumeroDossier,

		Nom:     patient.Nom,
		Prenoms: patient.Prenoms,
		Sexe:    patient.Sexe,

		DateNaissance: patient.DateNaissance,
		Age:           patient.Age,

		Telephone:       patient.Telephone,
		Quartier:        patient.Quartier,
		PersonneContact: patient.PersonneContact,

		IsAssure:        patient.IsAssure,
		TauxCouverture:  patient.TauxCouverture,
		MatriculeAssure: patient.MatriculeAssure,
	}
}

func ToSummary(patient Patient) PatientSummary {
	return PatientSummary{
		ID: patient.ID,

		CodePatient:   patient.CodePatient,
		NumeroDossier: patient.NumeroDossier,

		Nom:     patient.Nom,
		Prenoms: patient.Prenoms,
		Sexe:    patient.Sexe,

		DateNaissance: patient.DateNaissance,
		Age:           patient.Age,

		Telephone:       patient.Telephone,
		Quartier:        patient.Quartier,
		PersonneContact: patient.PersonneContact,

		IsAssure:        patient.IsAssure,
		TauxCouverture:  patient.TauxCouverture,
		MatriculeAssure: patient.MatriculeAssure,
	}
}

func ToSummaryList(patients []Patient) []PatientSummary {
	return coremapper.MapSlice(patients, ToSummary)
}
