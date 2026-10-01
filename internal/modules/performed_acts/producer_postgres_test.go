package performed_acts

import (
	"sync"
	"testing"

	"gorm.io/gorm"
)

func TestPostgresProducerMapUniquenessAndFK(t *testing.T) {
	db := performedActsDB(t)
	if err := EnsurePerformedActIndexes(db); err != nil {
		t.Fatal(err)
	}
	_, cat := seedPG(t, db)
	m := ProducerMap{
		SourceType: SourceConsultation, ClinicalKey: ConsultationClinicalKey,
		ActCatalogEntryID: cat.ID, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&m).Error; err != nil {
		t.Fatal(err)
	}
	dup := ProducerMap{
		SourceType: SourceConsultation, ClinicalKey: ConsultationClinicalKey,
		ActCatalogEntryID: cat.ID, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&dup).Error; err == nil {
		t.Fatal("duplicate producer map accepted")
	}
	orphan := ProducerMap{
		SourceType: SourceLaboratory, ClinicalKey: "NOPE",
		ActCatalogEntryID: 999999, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&orphan).Error; err == nil {
		t.Fatal("FK missing catalogue entry accepted")
	}
}

func TestPostgresProducerSourceUniquenessAndConcurrency(t *testing.T) {
	db := performedActsDB(t)
	if err := EnsurePerformedActIndexes(db); err != nil {
		t.Fatal(err)
	}
	p, cat := seedPG(t, db)
	if err := db.Create(&ProducerMap{
		SourceType: SourceLaboratory, ClinicalKey: "NFS", ActCatalogEntryID: cat.ID,
		IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	ids := make(chan uint, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var act *Act
			err := db.Transaction(func(tx *gorm.DB) error {
				var e error
				act, e = s.EnsureFromProducer(tx, ProducerCreateRequest{
					SourceType: SourceLaboratory, SourceID: 9001, PatientID: p.ID,
					ClinicalKey: "NFS", ActorID: 11,
				})
				return e
			})
			if err != nil {
				errs <- err
				return
			}
			ids <- act.ID
			errs <- nil
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	seen := map[uint]struct{}{}
	for id := range ids {
		seen[id] = struct{}{}
	}
	if len(seen) != 1 {
		t.Fatalf("expected single act id under concurrency, got %v", seen)
	}
	var n int64
	if err := db.Model(&Act{}).Where("source_type=? AND source_id=?", SourceLaboratory, 9001).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("row count=%d", n)
	}
}

func TestPostgresProducerRollbackWithoutMap(t *testing.T) {
	db := performedActsDB(t)
	if err := EnsurePerformedActIndexes(db); err != nil {
		t.Fatal(err)
	}
	p, _ := seedPG(t, db)
	s := NewService(db)

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS scratch_clinical (id BIGSERIAL PRIMARY KEY, status TEXT)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO scratch_clinical(status) VALUES ('PENDING')`).Error; err != nil {
			return err
		}
		_, err := s.EnsureFromProducer(tx, ProducerCreateRequest{
			SourceType: SourceLaboratory, SourceID: 1, PatientID: p.ID, ClinicalKey: "NO-MAP", ActorID: 1,
		})
		return err
	})
	if err == nil {
		t.Fatal("expected missing map failure")
	}
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM scratch_clinical`).Scan(&n).Error; err == nil && n != 0 {
		t.Fatalf("clinical scratch row survived rollback n=%d", n)
	}
	var acts int64
	_ = db.Model(&Act{}).Count(&acts)
	if acts != 0 {
		t.Fatalf("partial act n=%d", acts)
	}
}
