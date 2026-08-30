package main

import (
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
)

// seedDemoScheduling — LOT 23G.1 QA fixtures: appointment types + working schedules.
// Idempotent. Demo users already receive schedule.read.* from StaffFunctionPermissions at login.
func seedDemoScheduling(db *gorm.DB, actor uint) {
	now := time.Now().UTC()
	types := []patient_queue.AppointmentType{
		{Code: "CONSULT", Name: "Consultation", DefaultDurationMinutes: 30, Active: true, CreatedAt: now, UpdatedAt: now},
		{Code: "CTRL", Name: "Contrôle", DefaultDurationMinutes: 15, Active: true, CreatedAt: now, UpdatedAt: now},
		{Code: "URG-CONSULT", Name: "Consultation urgences", DefaultDurationMinutes: 20, Active: true, CreatedAt: now, UpdatedAt: now},
	}
	for _, t := range types {
		res := db.Exec(`
			INSERT INTO patient_queue_appointment_types
				(code, name, default_duration_minutes, active, created_at, updated_at)
			VALUES (?, ?, ?, true, ?, ?)
			ON CONFLICT (code) DO UPDATE SET
				name = EXCLUDED.name,
				default_duration_minutes = EXCLUDED.default_duration_minutes,
				active = true,
				updated_at = EXCLUDED.updated_at
		`, t.Code, t.Name, t.DefaultDurationMinutes, now, now)
		if res.Error != nil {
			log.Fatalf("appointment type %s: %v", t.Code, res.Error)
		}
	}

	var urgID uint
	if err := db.Table("organization_services").Select("id").Where("code = ?", "URG").Scan(&urgID).Error; err != nil || urgID == 0 {
		log.Fatal("service URG introuvable pour plannings DEMO")
	}

	emails := []string{
		"demo.generaliste@medcore.local",
		"demo.urgentiste@medcore.local",
	}
	validFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, email := range emails {
		var userID uint
		if err := db.Table("users").Select("id").Where("email = ?", email).Scan(&userID).Error; err != nil || userID == 0 {
			log.Printf("skip schedule seed: user %s introuvable", email)
			continue
		}
		for weekday := 0; weekday <= 6; weekday++ {
			row := patient_queue.StaffWorkingSchedule{
				PractitionerID: userID,
				ServiceID:      urgID,
				Weekday:        weekday,
				StartTime:      "08:00:00",
				EndTime:        "18:00:00",
				ValidFrom:      validFrom,
				Active:         true,
				CreatedBy:      actor,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			var existing patient_queue.StaffWorkingSchedule
			err := db.Where(
				"practitioner_id = ? AND service_id = ? AND weekday = ? AND start_time = ? AND valid_from = ?",
				userID, urgID, weekday, "08:00:00", validFrom,
			).FirstOrCreate(&existing, row).Error
			if err != nil {
				log.Fatalf("working schedule %s wd=%d: %v", email, weekday, err)
			}
			_ = db.Model(&existing).Updates(map[string]any{
				"end_time":   "18:00:00",
				"active":     true,
				"updated_at": now,
			}).Error
		}
	}
	log.Printf("Scheduling DEMO: types=%d, schedules for %d practitioners on URG", len(types), len(emails))
}
