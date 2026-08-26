package ticketing

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ticketTypes = map[string]string{"INCIDENT": "INC", "REQUEST": "REQ", "ACCESS_REQUEST": "ACC", "HARDWARE": "INC", "NETWORK": "INC", "APPLICATION": "INC", "OTHER": "REQ"}
var impacts = map[string]bool{"INDIVIDUAL": true, "SERVICE": true, "DEPARTMENT": true, "FACILITY": true}
var urgencies = map[string]bool{"LOW": true, "MEDIUM": true, "HIGH": true, "CRITICAL": true}
var priorities = map[string]bool{"P1": true, "P2": true, "P3": true, "P4": true}
var terminal = map[string]bool{"CLOSED": true, "CANCELLED": true}
var transitions = map[string]map[string]bool{
	"NEW": {"TRIAGED": true, "ASSIGNED": true, "CANCELLED": true}, "TRIAGED": {"ASSIGNED": true, "IN_PROGRESS": true, "CANCELLED": true},
	"ASSIGNED":     {"IN_PROGRESS": true, "WAITING_USER": true, "WAITING_THIRD_PARTY": true, "RESOLVED": true},
	"IN_PROGRESS":  {"WAITING_USER": true, "WAITING_THIRD_PARTY": true, "RESOLVED": true},
	"WAITING_USER": {"IN_PROGRESS": true, "RESOLVED": true, "CANCELLED": true}, "WAITING_THIRD_PARTY": {"IN_PROGRESS": true, "RESOLVED": true, "CANCELLED": true},
	"RESOLVED": {"CLOSED": true, "REOPENED": true}, "REOPENED": {"ASSIGNED": true, "IN_PROGRESS": true, "RESOLVED": true},
}

type Access struct {
	UserID      uint
	Permissions map[string]bool
	ServiceID   *uint
}

func (a Access) Has(p string) bool { return a.Permissions["*"] || a.Permissions[p] }

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func NewService(db *gorm.DB) *Service { return &Service{db: db, now: time.Now} }
func priorityFor(impact, urgency string) string {
	rank := map[string]int{"INDIVIDUAL": 1, "SERVICE": 2, "DEPARTMENT": 3, "FACILITY": 4}
	ur := map[string]int{"LOW": 1, "MEDIUM": 2, "HIGH": 3, "CRITICAL": 4}
	score := rank[impact] + ur[urgency]
	if score >= 7 {
		return "P1"
	}
	if score >= 5 {
		return "P2"
	}
	if score >= 3 {
		return "P3"
	}
	return "P4"
}
func defaults(p string) (int, int) {
	switch p {
	case "P1":
		return 15, 120
	case "P2":
		return 30, 240
	case "P3":
		return 240, 1440
	default:
		return 1440, 4320
	}
}
func (s *Service) sla(tx *gorm.DB, p string) (int, int, error) {
	var x SLA
	e := tx.Where("priority=? AND active", p).First(&x).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		a, b := defaults(p)
		return a, b, nil
	}
	return x.ResponseMinutes, x.ResolutionMinutes, e
}
func (s *Service) context(tx *gorm.DB, user uint) (*uint, *uint) {
	var x struct {
		ProfileID    uint
		ServiceID    *uint
		DepartmentID *uint
	}
	tx.Raw(`SELECT sp.id profile_id,sp.primary_service_id service_id,os.department_id FROM staff_profiles sp LEFT JOIN organization_services os ON os.id=sp.primary_service_id WHERE sp.user_id=? AND sp.active`, user).Scan(&x)
	if x.ProfileID == 0 {
		return nil, nil
	}
	return &x.ProfileID, x.ServiceID
}
func (s *Service) nextReference(tx *gorm.DB, typ string, now time.Time) (string, error) {
	prefix := ticketTypes[typ]
	var n int64
	key := fmt.Sprintf("%s-%d", prefix, now.Year())
	if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", key).Error; e != nil {
		return "", e
	}
	pattern := fmt.Sprintf("%s-%d-%%", prefix, now.Year())
	if e := tx.Model(&Ticket{}).Where("reference LIKE ?", pattern).Count(&n).Error; e != nil {
		return "", e
	}
	return fmt.Sprintf("%s-%d-%06d", prefix, now.Year(), n+1), nil
}
func clean(v string, max int) string {
	v = strings.TrimSpace(v)
	if len(v) > max {
		return v[:max]
	}
	return v
}
func (s *Service) Create(r CreateRequest, user uint) (*Detail, error) {
	typ := strings.ToUpper(r.Type)
	if ticketTypes[typ] == "" {
		return nil, coreerrors.Validation("Type de ticket invalide", nil)
	}
	impact := strings.ToUpper(r.Impact)
	if !impacts[impact] {
		impact = "INDIVIDUAL"
	}
	urgency := strings.ToUpper(r.Urgency)
	if !urgencies[urgency] {
		urgency = "MEDIUM"
	}
	if clean(r.Title, 180) == "" || clean(r.Description, 10000) == "" {
		return nil, coreerrors.Validation("Titre et description requis", nil)
	}
	var id uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		now := s.now()
		ref, e := s.nextReference(tx, typ, now)
		if e != nil {
			return e
		}
		p := priorityFor(impact, urgency)
		rm, xm, e := s.sla(tx, p)
		if e != nil {
			return e
		}
		profile, service := s.context(tx, user)
		t := Ticket{Reference: ref, Type: typ, CategoryCode: clean(strings.ToUpper(r.CategoryCode), 60), Subcategory: clean(r.Subcategory, 80), Title: clean(r.Title, 180), Description: clean(r.Description, 10000), Status: "NEW", Priority: p, Impact: impact, Urgency: urgency, RequesterUserID: user, RequesterStaffProfileID: profile, ServiceID: service, ApplicationModule: clean(r.ApplicationModule, 80), PageURL: clean(r.PageURL, 500), RequestID: clean(r.RequestID, 120), FrontendVersion: clean(r.FrontendVersion, 80), PatientRef: clean(r.PatientRef, 80), EncounterRef: clean(r.EncounterRef, 80), ResponseDueAt: now.Add(time.Duration(rm) * time.Minute), ResolutionDueAt: now.Add(time.Duration(xm) * time.Minute), CreatedAt: now, UpdatedAt: now}
		if e = tx.Create(&t).Error; e != nil {
			return e
		}
		id = t.ID
		return tx.Create(&History{TicketID: id, ActorUserID: user, EventType: "CREATED", NewValue: ref, CreatedAt: now}).Error
	})
	if e != nil {
		return nil, e
	}
	return s.Detail(id, Access{UserID: user, Permissions: map[string]bool{"ticket.read.own": true}})
}
func (s *Service) permitted(t *Ticket, a Access) bool {
	if a.Has("ticket.read.all") {
		return true
	}
	if t.RequesterUserID == a.UserID && a.Has("ticket.read.own") {
		return true
	}
	if a.Has("ticket.read.service") && a.ServiceID != nil && t.ServiceID != nil && *a.ServiceID == *t.ServiceID {
		return true
	}
	return false
}
func (s *Service) base() *gorm.DB {
	return s.db.Table("ticketing_tickets t").Select(`t.*,COALESCE(r.name,'') requester_name,COALESCE(a.name,'') assigned_name,COALESCE(os.name,'') service_name`).Joins("LEFT JOIN users r ON r.id=t.requester_user_id").Joins("LEFT JOIN users a ON a.id=t.assigned_to_user_id").Joins("LEFT JOIN organization_services os ON os.id=t.service_id")
}
func (s *Service) List(f Filter, a Access) (*Page, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	q := s.base()
	if !a.Has("ticket.read.all") {
		if a.Has("ticket.read.service") && a.ServiceID != nil {
			q = q.Where("t.requester_user_id=? OR t.service_id=?", a.UserID, *a.ServiceID)
		} else {
			q = q.Where("t.requester_user_id=?", a.UserID)
		}
	}
	for col, val := range map[string]string{"t.status": f.Status, "t.priority": f.Priority, "t.type": f.Type, "t.category_code": f.Category} {
		if strings.TrimSpace(val) != "" {
			q = q.Where(col+"=?", strings.ToUpper(val))
		}
	}
	if value := strings.TrimSpace(f.Service); value != "" {
		if n, e := strconv.ParseUint(value, 10, 64); e == nil {
			q = q.Where("t.service_id=?", n)
		} else {
			q = q.Where("LOWER(os.name) LIKE ?", "%"+strings.ToLower(value)+"%")
		}
	}
	if value := strings.TrimSpace(f.Requester); value != "" {
		if n, e := strconv.ParseUint(value, 10, 64); e == nil {
			q = q.Where("t.requester_user_id=?", n)
		} else {
			q = q.Where("LOWER(r.name) LIKE ?", "%"+strings.ToLower(value)+"%")
		}
	}
	if value := strings.TrimSpace(f.Assigned); value != "" {
		if n, e := strconv.ParseUint(value, 10, 64); e == nil {
			q = q.Where("t.assigned_to_user_id=?", n)
		} else {
			x := "%" + strings.ToLower(value) + "%"
			q = q.Where("LOWER(CONCAT(COALESCE(a.name,''),' ',COALESCE(t.assigned_queue,''))) LIKE ?", x)
		}
	}
	if f.Search != "" {
		x := "%" + strings.ToLower(strings.TrimSpace(f.Search)) + "%"
		q = q.Where("LOWER(CONCAT(t.reference,' ',t.title,' ',t.description)) LIKE ?", x)
	}
	now := s.now()
	if f.SLABreached == "true" {
		q = q.Where("(t.first_response_at IS NULL AND t.response_due_at<?) OR (t.resolved_at IS NULL AND t.resolution_due_at<?)", now, now)
	}
	var total int64
	if e := q.Session(&gorm.Session{}).Count(&total).Error; e != nil {
		return nil, e
	}
	var rows []TicketView
	if e := q.Order("t.created_at DESC").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Scan(&rows).Error; e != nil {
		return nil, e
	}
	for i := range rows {
		rows[i].ResponseSLABreached = rows[i].FirstResponseAt == nil && now.After(rows[i].ResponseDueAt)
		rows[i].ResolutionSLABreached = rows[i].ResolvedAt == nil && now.After(rows[i].ResolutionDueAt)
	}
	return &Page{Items: rows, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: int((total + int64(f.Limit) - 1) / int64(f.Limit))}, nil
}
func (s *Service) Detail(id uint, a Access) (*Detail, error) {
	var v TicketView
	if e := s.base().Where("t.id=?", id).Scan(&v).Error; e != nil {
		return nil, e
	}
	if v.ID == 0 {
		return nil, coreerrors.NotFound("TICKET")
	}
	if !s.permitted(&v.Ticket, a) {
		return nil, coreerrors.NotFound("TICKET")
	}
	now := s.now()
	v.ResponseSLABreached = v.FirstResponseAt == nil && now.After(v.ResponseDueAt)
	v.ResolutionSLABreached = v.ResolvedAt == nil && now.After(v.ResolutionDueAt)
	d := &Detail{TicketView: v, Comments: []Comment{}, Attachments: []Attachment{}, History: []History{}, Assignments: []Assignment{}}
	cq := s.db.Where("ticket_id=?", id)
	if !a.Has("ticket.comment.internal") {
		cq = cq.Where("visibility='PUBLIC'")
	}
	cq.Order("created_at").Find(&d.Comments)
	s.db.Where("ticket_id=?", id).Order("created_at").Find(&d.Attachments)
	if a.Has("ticket.audit.read") || a.Has("ticket.update") {
		s.db.Where("ticket_id=?", id).Order("created_at").Find(&d.History)
		s.db.Where("ticket_id=?", id).Order("created_at").Find(&d.Assignments)
	}
	return d, nil
}
func (s *Service) locked(tx *gorm.DB, id uint, a Access) (*Ticket, error) {
	var t Ticket
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, id).Error; e != nil {
		return nil, coreerrors.NotFound("TICKET")
	}
	if !s.permitted(&t, a) {
		return nil, coreerrors.NotFound("TICKET")
	}
	return &t, nil
}
func hist(tx *gorm.DB, id, actor uint, event, field, old, new string, now time.Time) error {
	return tx.Create(&History{TicketID: id, ActorUserID: actor, EventType: event, Field: field, OldValue: old, NewValue: new, CreatedAt: now}).Error
}
func notify(tx *gorm.DB, user, ticket uint, event, message string, now time.Time) error {
	if user == 0 {
		return nil
	}
	return tx.Create(&Notification{UserID: user, TicketID: ticket, EventType: event, Message: clean(message, 255), CreatedAt: now}).Error
}
func (s *Service) AddComment(id uint, r CommentRequest, a Access) (*Comment, error) {
	vis := strings.ToUpper(r.Visibility)
	if vis == "" {
		vis = "PUBLIC"
	}
	if vis != "PUBLIC" && vis != "INTERNAL" {
		return nil, coreerrors.Validation("Visibilité invalide", nil)
	}
	if vis == "INTERNAL" && !a.Has("ticket.comment.internal") {
		return nil, coreerrors.Forbidden("Commentaire interne interdit")
	}
	content := clean(r.Content, 10000)
	if content == "" {
		return nil, coreerrors.Validation("Commentaire requis", nil)
	}
	var c Comment
	e := s.db.Transaction(func(tx *gorm.DB) error {
		t, e := s.locked(tx, id, a)
		if e != nil {
			return e
		}
		now := s.now()
		c = Comment{TicketID: id, AuthorUserID: a.UserID, Visibility: vis, Content: content, CreatedAt: now}
		if e = tx.Create(&c).Error; e != nil {
			return e
		}
		if t.FirstResponseAt == nil && t.RequesterUserID != a.UserID && a.Has("ticket.update") {
			t.FirstResponseAt = &now
			if e = tx.Save(t).Error; e != nil {
				return e
			}
		}
		if e = hist(tx, id, a.UserID, "COMMENT_ADDED", "visibility", "", vis, now); e != nil {
			return e
		}
		if vis == "PUBLIC" && t.RequesterUserID != a.UserID {
			return notify(tx, t.RequesterUserID, id, "COMMENT_ADDED", "Une réponse a été ajoutée à votre ticket", now)
		}
		return nil
	})
	return &c, e
}
func (s *Service) Assign(id uint, r AssignRequest, a Access) (*Detail, error) {
	if !a.Has("ticket.assign") {
		return nil, coreerrors.Forbidden("Assignation interdite")
	}
	e := s.db.Transaction(func(tx *gorm.DB) error {
		t, e := s.locked(tx, id, a)
		if e != nil {
			return e
		}
		now := s.now()
		old := t.AssignedToUserID
		oldq := t.AssignedQueue
		t.AssignedToUserID = r.UserID
		t.AssignedQueue = clean(strings.ToUpper(r.Queue), 60)
		t.AssignedAt = &now
		if t.Status == "NEW" || t.Status == "TRIAGED" {
			t.Status = "ASSIGNED"
		}
		if e = tx.Save(t).Error; e != nil {
			return e
		}
		if e = tx.Create(&Assignment{TicketID: id, FromUserID: old, ToUserID: r.UserID, FromQueue: oldq, ToQueue: t.AssignedQueue, ActorUserID: a.UserID, CreatedAt: now}).Error; e != nil {
			return e
		}
		if e = hist(tx, id, a.UserID, "ASSIGNED", "assignedTo", "", fmt.Sprint(r.UserID), now); e != nil {
			return e
		}
		if r.UserID != nil {
			return notify(tx, *r.UserID, id, "ASSIGNED", "Un ticket vous a été assigné", now)
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return s.Detail(id, a)
}

func (s *Service) Update(id uint, r UpdateRequest, a Access) (*Detail, error) {
	if !a.Has("ticket.update") {
		return nil, coreerrors.Forbidden("Modification interdite")
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		t, err := s.locked(tx, id, a)
		if err != nil {
			return err
		}
		if terminal[t.Status] {
			return coreerrors.Conflict("Un ticket finalisé est immuable")
		}
		now := s.now()
		if r.CategoryCode != nil {
			old := t.CategoryCode
			t.CategoryCode = clean(strings.ToUpper(*r.CategoryCode), 60)
			if err = hist(tx, id, a.UserID, "CATEGORY_CHANGED", "category", old, t.CategoryCode, now); err != nil {
				return err
			}
		}
		if r.Subcategory != nil {
			t.Subcategory = clean(*r.Subcategory, 80)
		}
		if r.Impact != nil {
			v := strings.ToUpper(*r.Impact)
			if !impacts[v] {
				return coreerrors.Validation("Impact invalide", nil)
			}
			t.Impact = v
		}
		if r.Urgency != nil {
			v := strings.ToUpper(*r.Urgency)
			if !urgencies[v] {
				return coreerrors.Validation("Urgence invalide", nil)
			}
			t.Urgency = v
		}
		oldPriority := t.Priority
		if r.Priority != nil {
			v := strings.ToUpper(*r.Priority)
			if !priorities[v] {
				return coreerrors.Validation("Priorité invalide", nil)
			}
			t.Priority = v
		} else {
			t.Priority = priorityFor(t.Impact, t.Urgency)
		}
		if oldPriority != t.Priority {
			rm, xm, e := s.sla(tx, t.Priority)
			if e != nil {
				return e
			}
			t.ResponseDueAt = t.CreatedAt.Add(time.Duration(rm) * time.Minute)
			t.ResolutionDueAt = t.CreatedAt.Add(time.Duration(xm) * time.Minute)
			if e = hist(tx, id, a.UserID, "PRIORITY_CHANGED", "priority", oldPriority, t.Priority, now); e != nil {
				return e
			}
		}
		t.UpdatedAt = now
		return tx.Save(t).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Detail(id, a)
}
func (s *Service) Workflow(id uint, r WorkflowRequest, a Access) (*Detail, error) {
	next := strings.ToUpper(r.Status)
	e := s.db.Transaction(func(tx *gorm.DB) error {
		t, e := s.locked(tx, id, a)
		if e != nil {
			return e
		}
		perm := "ticket.update"
		if next == "RESOLVED" {
			perm = "ticket.resolve"
		}
		if next == "CLOSED" {
			perm = "ticket.close"
		}
		if next == "REOPENED" {
			perm = "ticket.reopen"
		}
		if !a.Has(perm) {
			return coreerrors.Forbidden("Transition interdite")
		}
		if !transitions[t.Status][next] {
			return coreerrors.Conflict("Transition de ticket invalide")
		}
		now := s.now()
		old := t.Status
		t.Status = next
		if next == "RESOLVED" {
			if clean(r.ResolutionSummary, 10000) == "" {
				return coreerrors.Validation("Résumé de résolution requis", nil)
			}
			t.ResolutionSummary = clean(r.ResolutionSummary, 10000)
			t.ResolutionCode = clean(r.ResolutionCode, 60)
			t.ResolvedAt = &now
		}
		if next == "CLOSED" {
			t.ClosedAt = &now
		}
		if next == "REOPENED" {
			t.ResolvedAt = nil
			t.ClosedAt = nil
		}
		if e = tx.Save(t).Error; e != nil {
			return e
		}
		if e = hist(tx, id, a.UserID, next, "status", old, next, now); e != nil {
			return e
		}
		return notify(tx, t.RequesterUserID, id, next, "Le statut de votre ticket est maintenant "+next, now)
	})
	if e != nil {
		return nil, e
	}
	return s.Detail(id, a)
}
func (s *Service) KPIs(a Access) (*KPIs, error) {
	if err := s.requireSupportScope(a); err != nil {
		return nil, err
	}
	q := s.db.Model(&Ticket{})
	q = s.filterSupportTickets(q, a, "")
	k := &KPIs{}
	q.Session(&gorm.Session{}).Where("status NOT IN ?", []string{"CLOSED", "CANCELLED"}).Count(&k.Open)
	q.Session(&gorm.Session{}).Where("created_at::date=CURRENT_DATE").Count(&k.NewToday)
	q.Session(&gorm.Session{}).Where("priority IN ? AND status NOT IN ?", []string{"P1", "P2"}, []string{"CLOSED", "CANCELLED"}).Count(&k.P1P2)
	q.Session(&gorm.Session{}).Where("(first_response_at IS NULL AND response_due_at<?) OR (resolved_at IS NULL AND resolution_due_at<?)", s.now(), s.now()).Count(&k.SLABreached)
	q.Session(&gorm.Session{}).Where("resolved_at IS NOT NULL").Count(&k.Resolved)
	rq := s.db.Table("ticketing_history h").Joins("JOIN ticketing_tickets t ON t.id = h.ticket_id").Where("h.event_type = ?", "REOPENED")
	s.filterSupportTickets(rq, a, "t").Count(&k.Reopened)
	q.Select("COALESCE(AVG(EXTRACT(EPOCH FROM (first_response_at-created_at))/60),0)").Where("first_response_at IS NOT NULL").Scan(&k.AverageFirstResponseMinutes)
	q.Select("COALESCE(AVG(EXTRACT(EPOCH FROM (resolved_at-created_at))/60),0)").Where("resolved_at IS NOT NULL").Scan(&k.MTTRMinutes)
	return k, nil
}

// requireSupportScope : ticket.read.all OK ; ticket.read.service exige un ServiceID ;
// sans service exploitable → Forbidden (jamais de bascule globale implicite).
func (s *Service) requireSupportScope(a Access) error {
	if a.Has("ticket.read.all") {
		return nil
	}
	if !a.Has("ticket.read.service") {
		return coreerrors.Forbidden("KPI support interdits")
	}
	if a.ServiceID == nil {
		return coreerrors.Forbidden("Périmètre service requis pour les KPI")
	}
	return nil
}

func (s *Service) filterSupportTickets(q *gorm.DB, a Access, alias string) *gorm.DB {
	if a.Has("ticket.read.all") {
		return q
	}
	col := "service_id"
	if alias != "" {
		col = alias + ".service_id"
	}
	return q.Where(col+"=?", *a.ServiceID)
}
func (s *Service) Categories() ([]Category, error) {
	var x []Category
	e := s.db.Where("active").Order("type,name").Find(&x).Error
	return x, e
}
func (s *Service) Notifications(user uint) ([]Notification, error) {
	var items []Notification
	err := s.db.Where("user_id=?", user).Order("created_at DESC").Limit(50).Find(&items).Error
	return items, err
}
func (s *Service) Agents() ([]AgentOption, error) {
	var items []AgentOption
	err := s.db.Raw(`SELECT DISTINCT u.id user_id,u.name,COALESCE(os.name,'') service_name FROM users u LEFT JOIN staff_profiles sp ON sp.user_id=u.id AND sp.active LEFT JOIN organization_services os ON os.id=sp.primary_service_id LEFT JOIN staff_functions sf ON sf.profile_id=sp.id AND sf.active WHERE u.is_active AND (u.role='admin' OR sf.code IN ('SUPPORT_AGENT','SUPPORT_MANAGER')) ORDER BY u.name`).Scan(&items).Error
	return items, err
}
func (s *Service) SeedDefaults(user uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, x := range []SLA{{Priority: "P1", ResponseMinutes: 15, ResolutionMinutes: 120, Active: true, UpdatedBy: user}, {Priority: "P2", ResponseMinutes: 30, ResolutionMinutes: 240, Active: true, UpdatedBy: user}, {Priority: "P3", ResponseMinutes: 240, ResolutionMinutes: 1440, Active: true, UpdatedBy: user}, {Priority: "P4", ResponseMinutes: 1440, ResolutionMinutes: 4320, Active: true, UpdatedBy: user}} {
			if e := tx.Where("priority=?", x.Priority).FirstOrCreate(&x).Error; e != nil {
				return e
			}
		}
		for _, x := range []Category{{Code: "APPLICATION", Name: "Application MedCore", Type: "APPLICATION", Active: true}, {Code: "HARDWARE", Name: "Matériel", Type: "HARDWARE", Active: true}, {Code: "NETWORK", Name: "Réseau", Type: "NETWORK", Active: true}, {Code: "ACCESS", Name: "Accès et droits", Type: "ACCESS_REQUEST", Active: true}, {Code: "OTHER", Name: "Autre", Type: "OTHER", Active: true}} {
			if e := tx.Where("code=?", x.Code).FirstOrCreate(&x).Error; e != nil {
				return e
			}
		}
		return nil
	})
}
