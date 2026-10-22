package main

import (
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

// Demo account + staff profile are created in ONE transaction (createDemoAccount):
// a profile failure rolls the user back, and a pre-existing account is never touched.

type profileSnapshot struct {
	EmployeeCode, JobTitle, PrimaryDepartment string
	Active                                    bool
	UpdatedAt                                 int64
	Functions                                 string
}

func snapshotProfile(t *testing.T, db *gorm.DB, userID uint) profileSnapshot {
	t.Helper()
	var p staff.Profile
	if err := db.Where("user_id=?", userID).First(&p).Error; err != nil {
		t.Fatalf("profile of user %d: %v", userID, err)
	}
	var fns string
	db.Raw("SELECT COALESCE(string_agg(code||':'||active::text, ',' ORDER BY code), '') FROM staff_functions WHERE profile_id=?", p.ID).Scan(&fns)
	return profileSnapshot{p.EmployeeCode, p.JobTitle, p.PrimaryDepartment, p.Active, p.UpdatedAt.UnixNano(), fns}
}

func countUnscoped(db *gorm.DB, email string) int64 {
	var n int64
	db.Unscoped().Model(&auth.User{}).Where("email=?", email).Count(&n)
	return n
}

func TestCreateDemoStaffAccountCreatesUserAndProfileTogether(t *testing.T) {
	db := seedUsersDB(t)
	withDemoHash(t, "demo-only-passphrase-42")
	item := demoStaffSpec{code: "DEMO-ATOMIC", name: "Atomique DEMO", email: "demo.atomic@medcore.local", title: "Infirmier", department: "Soins", functions: []string{"INFIRMIER"}, serviceCode: "URG"}
	created, err := createDemoStaffAccount(db, item, 1)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	u := snapshotUser(t, db, item.email)
	if u.Role != "staff" || !u.IsActive || u.Name != item.name {
		t.Fatalf("user: %+v", u)
	}
	var user auth.User
	db.Where("email=?", item.email).First(&user)
	p := snapshotProfile(t, db, user.ID)
	if p.EmployeeCode != "DEMO-ATOMIC" || !p.Active || p.Functions != "INFIRMIER:true" {
		t.Fatalf("profile: %+v", p)
	}
	var profiles int64
	db.Model(&staff.Profile{}).Where("user_id=?", user.ID).Count(&profiles)
	if profiles != 1 {
		t.Fatalf("profiles for user=%d", profiles)
	}
}

func TestCreateDemoAccountRollsBackUserWhenProfileFails(t *testing.T) {
	db := seedUsersDB(t)
	withDemoHash(t, "demo-only-passphrase-42")
	cases := map[string]demoStaffSpec{
		// Rejected by staff.Upsert validation, after the user row was inserted in the transaction.
		"invalid_function": {code: "DEMO-BAD-FN", name: "Bad FN", email: "demo.badfn@medcore.local", title: "x", functions: []string{"NOT_A_FUNCTION"}},
		// Unknown organization service: the profile cannot be built.
		"unknown_service": {code: "DEMO-BAD-SVC", name: "Bad SVC", email: "demo.badsvc@medcore.local", title: "x", functions: []string{"INFIRMIER"}, serviceCode: "NOPE"},
	}
	for name, item := range cases {
		t.Run(name, func(t *testing.T) {
			created, err := createDemoStaffAccount(db, item, 1)
			if err == nil || created {
				t.Fatalf("expected failure, created=%v err=%v", created, err)
			}
			if n := countUnscoped(db, item.email); n != 0 {
				t.Fatalf("partial user left behind: %d row(s)", n)
			}
			var profiles int64
			db.Model(&staff.Profile{}).Where("employee_code=?", item.code).Count(&profiles)
			if profiles != 0 {
				t.Fatal("partial profile left behind")
			}
		})
	}
	// Inactive variant: the is_active=false update and the user insert roll back too.
	boom := errors.New("profile failure")
	if _, created, err := createDemoAccount(db, "Inactif", "demo.inactive.fail@medcore.local", "staff", false, func(*gorm.DB, auth.User) error { return boom }); !errors.Is(err, boom) || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if n := countUnscoped(db, "demo.inactive.fail@medcore.local"); n != 0 {
		t.Fatal("inactive partial user left behind")
	}
	// The failure did not poison later creations.
	if created, err := createDemoStaffAccount(db, demoStaffSpec{code: "DEMO-OK", name: "OK", email: "demo.ok@medcore.local", functions: []string{"INFIRMIER"}}, 1); err != nil || !created {
		t.Fatalf("after rollback: created=%v err=%v", created, err)
	}
}

func TestCreateDemoAccountLeavesExistingAccountAndProfileUntouched(t *testing.T) {
	db := seedUsersDB(t)
	withDemoHash(t, "demo-only-passphrase-42")
	otherHash, _ := bcrypt.GenerateFromPassword([]byte("someone-else-secret-1"), bcrypt.MinCost)
	user := auth.User{Name: "Infirmière renommée", Email: "demo.infirmier@medcore.local", PasswordHash: string(otherHash), Role: "accueil", IsActive: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	inactive := false
	if _, err := staff.NewService(db).Upsert(0, staff.UpsertRequest{UserID: user.ID, EmployeeCode: "OPS-042", JobTitle: "Poste exploitant", PrimaryDepartment: "Exploitation", Active: &inactive, Functions: []string{"ACCUEIL"}}, 1); err != nil {
		t.Fatal(err)
	}
	db.Model(&user).Update("is_active", false)
	beforeUser := snapshotUser(t, db, user.Email)
	beforeProfile := snapshotProfile(t, db, user.ID)

	item := demoStaffSpec{code: "DEMO-INFIRMIER", name: "Infirmier DEMO", email: user.Email, title: "Infirmier", department: "Soins", functions: []string{"INFIRMIER"}, serviceCode: "URG"}
	for i := 0; i < 2; i++ {
		created, err := createDemoStaffAccount(db, item, 1)
		if err != nil || created {
			t.Fatalf("run %d: created=%v err=%v", i, created, err)
		}
	}
	if _, _, err := createDemoAccount(db, "x", user.Email, "admin", true, func(*gorm.DB, auth.User) error {
		t.Fatal("profile callback must not run for an existing account")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if after := snapshotUser(t, db, user.Email); after != beforeUser {
		t.Fatalf("existing user modified:\nbefore=%+v\nafter=%+v", beforeUser, after)
	}
	if after := snapshotProfile(t, db, user.ID); after != beforeProfile {
		t.Fatalf("existing profile modified:\nbefore=%+v\nafter=%+v", beforeProfile, after)
	}
	if bcrypt.CompareHashAndPassword([]byte(beforeUser.PasswordHash), []byte("demo-only-passphrase-42")) == nil {
		t.Fatal("demo password must not open an existing account")
	}
}

func TestCreateDemoAccountLeavesSoftDeletedAccountUntouched(t *testing.T) {
	db := seedUsersDB(t)
	withDemoHash(t, "demo-only-passphrase-42")
	otherHash, _ := bcrypt.GenerateFromPassword([]byte("someone-else-secret-1"), bcrypt.MinCost)
	user := auth.User{Name: "Ancien caissier", Email: "demo.caissiere@medcore.local", PasswordHash: string(otherHash), Role: "admin", IsActive: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	db.Model(&user).Update("is_active", false)
	if err := db.Delete(&user).Error; err != nil {
		t.Fatal(err)
	}
	var before auth.User
	db.Unscoped().First(&before, user.ID)

	item := demoStaffSpec{code: "DEMO-CAISSIERE", name: "Caissière DEMO", email: user.Email, title: "Caissière", department: "Caisse", functions: []string{"CAISSIER"}}
	for i := 0; i < 2; i++ {
		if created, err := createDemoStaffAccount(db, item, 1); err != nil || created {
			t.Fatalf("run %d: created=%v err=%v", i, created, err)
		}
	}
	var after auth.User
	db.Unscoped().First(&after, user.ID)
	if !after.DeletedAt.Valid || !after.DeletedAt.Time.Equal(before.DeletedAt.Time) {
		t.Fatal("soft-deleted account must stay deleted")
	}
	if after.Name != before.Name || after.Role != before.Role || after.PasswordHash != before.PasswordHash || after.IsActive || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("soft-deleted account modified:\nbefore=%+v\nafter=%+v", before, after)
	}
	if n := countUnscoped(db, user.Email); n != 1 {
		t.Fatalf("duplicate account rows=%d", n)
	}
	var profiles int64
	db.Model(&staff.Profile{}).Where("user_id=?", user.ID).Count(&profiles)
	if profiles != 0 {
		t.Fatal("seed must not create/repair a profile for a soft-deleted account")
	}
}
