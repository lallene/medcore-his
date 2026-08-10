package medical_records

import (
	"encoding/json"
	"testing"
)

func TestUpdateCommonMedicalRecordRequestDistinguishesAbsentAndEmptyCollection(t *testing.T) {
	var absent UpdateCommonMedicalRecordRequest
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.Allergies.Present {
		t.Fatal("an absent collection must not be marked present")
	}

	var empty UpdateCommonMedicalRecordRequest
	if err := json.Unmarshal([]byte(`{"allergies":{"upsert":[],"delete_ids":[]}}`), &empty); err != nil {
		t.Fatal(err)
	}
	if !empty.Allergies.Present {
		t.Fatal("an explicit empty collection must be marked present")
	}
	if len(empty.Allergies.Upsert) != 0 || len(empty.Allergies.DeleteIDs) != 0 {
		t.Fatal("an explicit empty collection must not imply a mutation")
	}
}

func TestPatchCollectionAcceptsLegacyArrayAsUpsertOnly(t *testing.T) {
	var request UpdateCommonMedicalRecordRequest
	payload := `{"allergies":[{"id":12,"allergen_name":"Pénicilline"}]}`
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		t.Fatal(err)
	}
	if !request.Allergies.Present || len(request.Allergies.Upsert) != 1 {
		t.Fatal("legacy array must be decoded as an upsert collection")
	}
	if len(request.Allergies.DeleteIDs) != 0 {
		t.Fatal("legacy array must never infer deletions")
	}
	if request.Allergies.Upsert[0].ID != 12 {
		t.Fatal("legacy item id was not preserved")
	}
}

func TestPatchDTOsPreserveFalseEmptyAndNull(t *testing.T) {
	var request UpdateCommonMedicalRecordRequest
	payload := `{
		"allergies":{"upsert":[{"id":1,"comment":"","is_active":false}]},
		"vital_signs":{"upsert":[{"id":2,"temperature_c":null,"consultation_id":null}]},
		"documents":{"upsert":[{"id":3,"document_date":null,"file_name":""}]}
	}`
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		t.Fatal(err)
	}

	allergy := request.Allergies.Upsert[0]
	if allergy.Comment == nil || *allergy.Comment != "" {
		t.Fatal("explicit empty string was not preserved")
	}
	if allergy.IsActive == nil || *allergy.IsActive {
		t.Fatal("explicit false was not preserved")
	}

	vital := request.VitalSigns.Upsert[0]
	if !vital.TemperatureC.Set || vital.TemperatureC.Value != nil {
		t.Fatal("explicit nullable scalar null was not preserved")
	}
	if !vital.ConsultationID.Set || vital.ConsultationID.Value != nil {
		t.Fatal("explicit nullable foreign key null was not preserved")
	}

	document := request.Documents.Upsert[0]
	if !document.DocumentDate.Set || document.DocumentDate.Value != nil {
		t.Fatal("explicit nullable date null was not preserved")
	}
	if document.FileName == nil || *document.FileName != "" {
		t.Fatal("explicit document filename clearing was not preserved")
	}
}

func TestPatchDTOsLeaveFieldsAbsent(t *testing.T) {
	var request UpdateCommonMedicalRecordRequest
	if err := json.Unmarshal([]byte(`{"vital_signs":{"upsert":[{"id":2}]}}`), &request); err != nil {
		t.Fatal(err)
	}
	vital := request.VitalSigns.Upsert[0]
	if vital.TemperatureC.Set || vital.ConsultationID.Set || vital.Comment != nil {
		t.Fatal("absent fields must remain absent")
	}
}
