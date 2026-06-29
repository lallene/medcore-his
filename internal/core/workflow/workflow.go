package workflow

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type State string
type Action string
type Permission string

type Actor struct {
	UserID      uint
	Role        string
	Permissions []string
}

type Transition struct {
	Action      Action
	From        []State
	To          State
	Permission  Permission
	Description string
}

type Definition struct {
	Name        string
	Initial     State
	EntityName  string
	Transitions []Transition
}

type Entity interface {
	GetWorkflowState() State
	SetWorkflowState(State)
	GetWorkflowID() uint
}

type Validator func(entity Entity, transition Transition, actor Actor) error

type Engine struct {
	db         *gorm.DB
	definition Definition
	validator  Validator
}

func New(db *gorm.DB, definition Definition, validator Validator) *Engine {
	return &Engine{
		db:         db,
		definition: definition,
		validator:  validator,
	}
}

func (e *Engine) Apply(entity Entity, action Action, actor Actor, reason string) error {
	fromState := entity.GetWorkflowState()

	transition, err := e.findTransition(fromState, action)
	if err != nil {
		return err
	}

	if !actorCan(actor, transition.Permission) {
		return fmt.Errorf("permission refusée: %s", transition.Permission)
	}

	if e.validator != nil {
		if err := e.validator(entity, transition, actor); err != nil {
			return err
		}
	}

	entity.SetWorkflowState(transition.To)

	if e.db != nil {
		userID := actor.UserID

		history := History{
			WorkflowName: e.definition.Name,
			EntityName:   e.definition.EntityName,
			EntityID:     entity.GetWorkflowID(),
			FromState:    string(fromState),
			ToState:      string(transition.To),
			Action:       string(action),
			UserID:       &userID,
			Role:         actor.Role,
			Reason:       reason,
			OccurredAt:   time.Now(),
		}

		if err := e.db.Create(&history).Error; err != nil {
			return err
		}
	}

	return nil
}

func (e *Engine) findTransition(current State, action Action) (Transition, error) {
	for _, transition := range e.definition.Transitions {
		if transition.Action != action {
			continue
		}

		for _, from := range transition.From {
			if from == current {
				return transition, nil
			}
		}
	}

	return Transition{}, fmt.Errorf(
		"transition interdite: action=%s depuis état=%s",
		action,
		current,
	)
}

func actorCan(actor Actor, permission Permission) bool {
	if permission == "" {
		return true
	}

	for _, item := range actor.Permissions {
		if item == string(permission) || item == "*" {
			return true
		}
	}

	return false
}
