package patients

import (
	"errors"
	"strings"
	"testing"

	coreRepo "github.com/lallene/medcore-his/backend/internal/core/repository"
	"gorm.io/gorm"
)

func TestNormalizePatientEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty", in: "", want: ""},
		{name: "whitespace", in: "   \t  ", want: ""},
		{name: "trim", in: "  patient@example.com  ", want: "patient@example.com"},
		{name: "preserve_case", in: "  Patient.Name@Example.COM ", want: "Patient.Name@Example.COM"},
		{name: "malformed", in: "not-an-email", wantErr: true},
		{name: "display_name", in: "John Doe <patient@example.com>", wantErr: true},
		{name: "angle_only", in: "<patient@example.com>", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizePatientEmail(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !errors.Is(err, errInvalidPatientEmail) {
					t.Fatalf("got %v, want errInvalidPatientEmail", err)
				}
				if strings.Contains(err.Error(), tc.in) && strings.TrimSpace(tc.in) != "" {
					t.Fatalf("error leaked input: %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizePatientEmailTooLong(t *testing.T) {
	t.Parallel()
	local := strings.Repeat("a", maxPatientEmailLen)
	_, err := NormalizePatientEmail(local + "@x.co")
	if !errors.Is(err, errInvalidPatientEmail) {
		t.Fatalf("got %v, want errInvalidPatientEmail", err)
	}
}

func TestNormalizePatientEmailExactMaxLen(t *testing.T) {
	t.Parallel()
	// Deterministic valid addr-spec of exactly 150 characters: local@example.com
	const domain = "@example.com"
	local := strings.Repeat("a", maxPatientEmailLen-len(domain))
	addr := local + domain
	if len(addr) != maxPatientEmailLen {
		t.Fatalf("fixture length=%d, want %d", len(addr), maxPatientEmailLen)
	}
	got, err := NormalizePatientEmail(addr)
	if err != nil {
		t.Fatal(err)
	}
	if got != addr {
		t.Fatalf("got %q", got)
	}
}

func TestMapperExposesCanonicalEmail(t *testing.T) {
	t.Parallel()
	p := &Patient{
		CodePatient: "P1", NumeroDossier: "D1", Nom: "DOE", Prenoms: "Jane",
		Email: "jane@example.com",
	}
	p.ID = 42
	resp := ToResponse(p)
	if resp.Email != "jane@example.com" {
		t.Fatalf("PatientResponse.Email=%q", resp.Email)
	}
	sum := ToSummary(*p)
	if sum.Email != "jane@example.com" {
		t.Fatalf("PatientSummary.Email=%q", sum.Email)
	}
}

func TestCreatePatientEmail(t *testing.T) {
	t.Parallel()

	t.Run("without_email", func(t *testing.T) {
		svc := NewService(newMemPatientRepo())
		p, err := svc.Create(CreatePatientRequest{Nom: "Alpha", Prenoms: "A"})
		if err != nil {
			t.Fatal(err)
		}
		if p.Email != "" {
			t.Fatalf("email=%q, want empty", p.Email)
		}
	})

	t.Run("valid_email", func(t *testing.T) {
		svc := NewService(newMemPatientRepo())
		p, err := svc.Create(CreatePatientRequest{Nom: "Beta", Email: "beta@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if p.Email != "beta@example.com" {
			t.Fatalf("email=%q", p.Email)
		}
	})

	t.Run("trim_whitespace", func(t *testing.T) {
		svc := NewService(newMemPatientRepo())
		p, err := svc.Create(CreatePatientRequest{Nom: "Gamma", Email: "  gamma@example.com  "})
		if err != nil {
			t.Fatal(err)
		}
		if p.Email != "gamma@example.com" {
			t.Fatalf("email=%q", p.Email)
		}
	})

	t.Run("malformed_rejected", func(t *testing.T) {
		repo := newMemPatientRepo()
		svc := NewService(repo)
		_, err := svc.Create(CreatePatientRequest{Nom: "Delta", Email: "not-an-email"})
		if !errors.Is(err, errInvalidPatientEmail) {
			t.Fatalf("got %v", err)
		}
		if repo.countStored() != 0 {
			t.Fatal("invalid create must not persist")
		}
	})

	t.Run("display_name_rejected", func(t *testing.T) {
		repo := newMemPatientRepo()
		svc := NewService(repo)
		_, err := svc.Create(CreatePatientRequest{Nom: "Epsilon", Email: "John Doe <eps@example.com>"})
		if !errors.Is(err, errInvalidPatientEmail) {
			t.Fatalf("got %v", err)
		}
		if repo.countStored() != 0 {
			t.Fatal("invalid create must not persist")
		}
	})

	t.Run("duplicate_email_allowed", func(t *testing.T) {
		svc := NewService(newMemPatientRepo())
		shared := "family@example.com"
		a, err := svc.Create(CreatePatientRequest{Nom: "One", Email: shared})
		if err != nil {
			t.Fatal(err)
		}
		b, err := svc.Create(CreatePatientRequest{Nom: "Two", Email: shared})
		if err != nil {
			t.Fatal(err)
		}
		if a.Email != shared || b.Email != shared {
			t.Fatalf("emails %q %q", a.Email, b.Email)
		}
		if a.ID == b.ID {
			t.Fatal("expected distinct patients")
		}
	})
}

func TestUpdatePatientEmail(t *testing.T) {
	t.Parallel()

	seed := func(t *testing.T) (Service, uint) {
		t.Helper()
		svc := NewService(newMemPatientRepo())
		p, err := svc.Create(CreatePatientRequest{Nom: "Seed", Email: "old@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		return svc, p.ID
	}

	t.Run("valid_replacement", func(t *testing.T) {
		svc, id := seed(t)
		next := "new@example.com"
		p, err := svc.Update(id, UpdatePatientRequest{Nom: "Seed", Email: &next})
		if err != nil {
			t.Fatal(err)
		}
		if p.Email != next {
			t.Fatalf("email=%q", p.Email)
		}
	})

	t.Run("whitespace_normalization", func(t *testing.T) {
		svc, id := seed(t)
		next := "  New@Example.com  "
		p, err := svc.Update(id, UpdatePatientRequest{Nom: "Seed", Email: &next})
		if err != nil {
			t.Fatal(err)
		}
		if p.Email != "New@Example.com" {
			t.Fatalf("email=%q", p.Email)
		}
	})

	t.Run("explicit_empty_clears", func(t *testing.T) {
		svc, id := seed(t)
		empty := ""
		p, err := svc.Update(id, UpdatePatientRequest{Nom: "Seed", Email: &empty})
		if err != nil {
			t.Fatal(err)
		}
		if p.Email != "" {
			t.Fatalf("email=%q, want cleared", p.Email)
		}
	})

	t.Run("whitespace_only_clears", func(t *testing.T) {
		svc, id := seed(t)
		ws := "   "
		p, err := svc.Update(id, UpdatePatientRequest{Nom: "Seed", Email: &ws})
		if err != nil {
			t.Fatal(err)
		}
		if p.Email != "" {
			t.Fatalf("email=%q, want cleared", p.Email)
		}
	})

	t.Run("omitted_preserves", func(t *testing.T) {
		svc, id := seed(t)
		p, err := svc.Update(id, UpdatePatientRequest{Nom: "SEED-UPDATED", Email: nil})
		if err != nil {
			t.Fatal(err)
		}
		if p.Email != "old@example.com" {
			t.Fatalf("omitted email cleared stored value: got %q", p.Email)
		}
		if p.Nom != "SEED-UPDATED" {
			t.Fatalf("nom=%q", p.Nom)
		}
	})

	t.Run("invalid_replacement_rejected", func(t *testing.T) {
		svc, id := seed(t)
		bad := "John <bad@example.com>"
		_, err := svc.Update(id, UpdatePatientRequest{Nom: "Seed", Email: &bad})
		if !errors.Is(err, errInvalidPatientEmail) {
			t.Fatalf("got %v", err)
		}
		got, err := svc.FindByID(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Email != "old@example.com" {
			t.Fatalf("invalid update mutated email to %q", got.Email)
		}
	})
}

// memPatientRepo is an in-memory patients.Repository for email service tests.
type memPatientRepo struct {
	byID   map[uint]*Patient
	nextID uint
}

func newMemPatientRepo() *memPatientRepo {
	return &memPatientRepo{byID: make(map[uint]*Patient)}
}

func (m *memPatientRepo) countStored() int { return len(m.byID) }

func (m *memPatientRepo) clone(p *Patient) *Patient {
	cp := *p
	return &cp
}

func (m *memPatientRepo) Create(entity *Patient) error {
	m.nextID++
	entity.ID = m.nextID
	m.byID[entity.ID] = m.clone(entity)
	return nil
}

func (m *memPatientRepo) Update(entity *Patient) error {
	if _, ok := m.byID[entity.ID]; !ok {
		return gorm.ErrRecordNotFound
	}
	m.byID[entity.ID] = m.clone(entity)
	return nil
}

func (m *memPatientRepo) Delete(id uint) error {
	delete(m.byID, id)
	return nil
}

func (m *memPatientRepo) FindByID(id uint, _ ...coreRepo.Option) (*Patient, error) {
	p, ok := m.byID[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return m.clone(p), nil
}

func (m *memPatientRepo) FindAll(_ ...coreRepo.Option) ([]Patient, error) {
	out := make([]Patient, 0, len(m.byID))
	for _, p := range m.byID {
		out = append(out, *p)
	}
	return out, nil
}

func (m *memPatientRepo) Paginate(page, limit int, _ ...coreRepo.Option) (*coreRepo.PageResult[Patient], error) {
	all, _ := m.FindAll()
	return &coreRepo.PageResult[Patient]{Data: all, Page: page, Limit: limit, Total: int64(len(all))}, nil
}

func (m *memPatientRepo) Count(_ ...coreRepo.Option) (int64, error) {
	return int64(len(m.byID)), nil
}

func (m *memPatientRepo) Exists(id uint) (bool, error) {
	_, ok := m.byID[id]
	return ok, nil
}

func (m *memPatientRepo) Transaction(fn func(tx *gorm.DB) error) error { return fn(nil) }

func (m *memPatientRepo) DB() *gorm.DB { return nil }

func (m *memPatientRepo) FindByTelephone(telephone string) (*Patient, error) {
	for _, p := range m.byID {
		if p.Telephone == telephone {
			return m.clone(p), nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *memPatientRepo) FindByNumeroDossier(numeroDossier string) (*Patient, error) {
	for _, p := range m.byID {
		if p.NumeroDossier == numeroDossier {
			return m.clone(p), nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}
