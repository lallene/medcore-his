package performed_acts

import (
	"strings"

	"github.com/lallene/medcore-his/backend/internal/modules/examcategories"
)

// ProducerReadinessReport builds deployment readiness for configured producer maps.
// Lab/imaging required universe = active medical_exams matching domain category filters
// (shared examcategories helpers used by laboratory/imaging Materialize).
// Does not invent mappings.
func (s *Service) ProducerReadinessReport() (*ProducerReadiness, error) {
	out := &ProducerReadiness{
		ProducersEnabled: s.ProducersEnabled(),
		Producers:        map[string]ProducerReadinessDetail{},
		Missing:          []ProducerMapGap{},
		Invalid:          []ProducerMapGap{},
	}

	maps, err := s.ListProducerMaps()
	if err != nil {
		return nil, err
	}
	byKey := map[string]ProducerMap{}
	for _, m := range maps {
		byKey[m.SourceType+"\x00"+m.ClinicalKey] = m
	}

	consult := s.evaluateRequired(SourceConsultation, []string{ConsultationClinicalKey}, byKey)
	out.Producers[SourceConsultation] = consult
	out.Missing = append(out.Missing, consult.Missing...)
	out.Invalid = append(out.Invalid, consult.Invalid...)

	labCodes, err := s.activeExamCodes(examcategories.IsLaboratory)
	if err != nil {
		return nil, err
	}
	lab := s.evaluateRequired(SourceLaboratory, labCodes, byKey)
	out.Producers[SourceLaboratory] = lab
	out.Missing = append(out.Missing, lab.Missing...)
	out.Invalid = append(out.Invalid, lab.Invalid...)

	imgCodes, err := s.activeExamCodes(examcategories.IsImaging)
	if err != nil {
		return nil, err
	}
	img := s.evaluateRequired(SourceImaging, imgCodes, byKey)
	out.Producers[SourceImaging] = img
	out.Missing = append(out.Missing, img.Missing...)
	out.Invalid = append(out.Invalid, img.Invalid...)

	out.Ready = consult.Ready && lab.Ready && img.Ready
	return out, nil
}

func (s *Service) activeExamCodes(match func(string) bool) ([]string, error) {
	if !s.db.Migrator().HasTable("medical_exams") {
		return []string{}, nil
	}
	type row struct {
		Code     string
		Category string
	}
	var rows []row
	if err := s.db.Table("medical_exams").
		Select("code, category").
		Where("is_active = ?", true).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var codes []string
	for _, r := range rows {
		code := strings.TrimSpace(r.Code)
		if code == "" || !match(r.Category) {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes, nil
}

func (s *Service) evaluateRequired(sourceType string, clinicalKeys []string, byKey map[string]ProducerMap) ProducerReadinessDetail {
	detail := ProducerReadinessDetail{
		RequiredCount: len(clinicalKeys),
		Missing:       []ProducerMapGap{},
		Invalid:       []ProducerMapGap{},
	}
	configured := 0
	for _, key := range clinicalKeys {
		m, ok := byKey[sourceType+"\x00"+key]
		if !ok {
			detail.Missing = append(detail.Missing, ProducerMapGap{
				SourceType: sourceType, ClinicalKey: key, Reason: "MISSING_MAP",
			})
			continue
		}
		configured++
		if !m.IsActive {
			detail.Invalid = append(detail.Invalid, ProducerMapGap{
				SourceType: sourceType, ClinicalKey: key, Reason: "MAP_INACTIVE",
			})
			continue
		}
		var catalog struct {
			ID       uint
			IsActive bool
		}
		err := s.db.Table("act_catalog_entries").
			Select("id, is_active").
			Where("id = ?", m.ActCatalogEntryID).
			Take(&catalog).Error
		if err != nil || catalog.ID == 0 {
			detail.Invalid = append(detail.Invalid, ProducerMapGap{
				SourceType: sourceType, ClinicalKey: key, Reason: "CATALOG_MISSING",
			})
			continue
		}
		if !catalog.IsActive {
			detail.Invalid = append(detail.Invalid, ProducerMapGap{
				SourceType: sourceType, ClinicalKey: key, Reason: "CATALOG_INACTIVE",
			})
			continue
		}
	}
	detail.ConfiguredCount = configured
	detail.Ready = len(detail.Missing) == 0 && len(detail.Invalid) == 0
	return detail
}
