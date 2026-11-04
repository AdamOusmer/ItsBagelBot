// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"slices"
	"strings"

	"ItsBagelBot/app/db/modules/ent"
	entautomation "ItsBagelBot/app/db/modules/ent/automation"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"entgo.io/ent/dialect"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	ErrAutomationNotFound = errors.New("automation not found")
	ErrRevisionConflict   = errors.New("automation revision conflict")
)

type AutomationRefusal struct {
	Code   rpc.Code
	Issues []automation.Issue
}

func (e *AutomationRefusal) Error() string {
	return "automation refused: " + string(e.Code)
}

type AutomationInput struct {
	ID               string
	Name             string
	Enabled          bool
	Definition       []byte
	Preview          string
	ExpectedRevision *int64
	lockChannelCap   bool
}

type Automations struct {
	client   *ent.Client
	pub      bus.Publisher
	modules  *Modules
	log      *zap.Logger
	rowLocks bool
}

// SetDialect turns on the channel cap row lock; SQLite test databases have no FOR UPDATE.
func (a *Automations) SetDialect(name string) {
	a.rowLocks = name == dialect.MySQL
}

func NewAutomations(client *ent.Client, pub bus.Publisher, log *zap.Logger) *Automations {
	return &Automations{client: client, pub: pub, log: log.Named("automations")}
}

func automationView(row *ent.Automation) (automation.Automation, error) {
	var def automation.Definition
	if err := codec.Unmarshal([]byte(row.Definition), &def); err != nil {
		return automation.Automation{}, err
	}
	return automation.Automation{
		ID: row.ID, UserID: row.UserID, Name: row.Name, Enabled: row.Enabled,
		Revision: row.Revision, Definition: def,
		CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}, nil
}

func automationViews(rows []*ent.Automation) ([]automation.Automation, error) {
	out := make([]automation.Automation, len(rows))
	for i, row := range rows {
		view, err := automationView(row)
		if err != nil {
			return nil, err
		}
		out[i] = view
	}
	return out, nil
}

func (a *Automations) List(ctx context.Context, userID uint64) ([]automation.Automation, error) {
	rows, err := a.client.Automation.Query().
		Where(entautomation.UserID(userID)).
		Order(ent.Asc(entautomation.FieldNameKey)).
		Select(runtimeColumns...).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return automationViews(rows)
}

func (a *Automations) Get(ctx context.Context, userID uint64, id string) (*automation.Automation, bool, error) {
	row, err := a.client.Automation.Query().
		Where(entautomation.UserID(userID), entautomation.ID(id)).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	view, err := automationView(row)
	if err != nil {
		return nil, false, err
	}
	return &view, true, nil
}

func (a *Automations) Validate(ctx context.Context, userID uint64, name string, raw []byte) ([]automation.Issue, error) {
	_, issues := parseAutomation(name, raw)
	if len(issues) > 0 {
		return issues, nil
	}
	def, _ := automation.Parse(raw)
	known, err := knownAutomationIDs(ctx, a.client.Automation, userID)
	if err != nil {
		return nil, err
	}
	return sortIssues(unknownRunTargets(def, known)), nil
}

func (a *Automations) Upsert(ctx context.Context, userID uint64, in AutomationInput) (automation.Automation, error) {
	in.Name = strings.TrimSpace(in.Name)
	def, issues := parseAutomationInput(in)
	if len(issues) > 0 {
		return automation.Automation{}, &AutomationRefusal{Code: rpc.CodeInvalid, Issues: issues}
	}
	tx, err := a.client.Tx(ctx)
	if err != nil {
		return automation.Automation{}, err
	}
	defer tx.Rollback()
	in.lockChannelCap = a.rowLocks
	row, err := writeAutomation(ctx, tx, automationDraft{userID: userID, in: in, def: def})
	if err != nil {
		return automation.Automation{}, mapAutomationConstraint(err)
	}
	if err := tx.Commit(); err != nil {
		return automation.Automation{}, mapAutomationConstraint(err)
	}
	view, err := automationView(row)
	if err != nil {
		return automation.Automation{}, err
	}
	a.announce(ctx, data.AutomationChangedDTO{UserID: userID, ID: view.ID, Automation: &view})
	return view, nil
}

func (a *Automations) Delete(ctx context.Context, userID uint64, id string) (bool, error) {
	n, err := a.client.Automation.Delete().
		Where(entautomation.UserID(userID), entautomation.ID(id)).
		Exec(ctx)
	if err != nil || n == 0 {
		return false, err
	}
	a.announce(ctx, data.AutomationChangedDTO{UserID: userID, ID: id, Deleted: true})
	return true, nil
}

func (a *Automations) announce(ctx context.Context, dto data.AutomationChangedDTO) {
	if err := bus.PublishJSON(ctx, a.pub, data.SubjectAutomationChanged, dto); err != nil {
		a.log.Error("failed to publish automation change",
			zap.Uint64("user_id", dto.UserID), zap.String("automation_id", dto.ID), zap.Error(err))
	}
}

func parseAutomation(name string, raw []byte) (automation.Definition, []automation.Issue) {
	var issues []automation.Issue
	if len(raw) > automation.MaxDefinitionBytes {
		issues = append(issues, automation.Issue{Path: "definition", Code: "too_long"})
	}
	def, defIssues := automation.Parse(raw)
	issues = append(issues, defIssues...)
	issues = append(issues, automation.ValidateName(name)...)
	return def, sortIssues(issues)
}

func parseAutomationInput(in AutomationInput) (automation.Definition, []automation.Issue) {
	def, issues := parseAutomation(in.Name, in.Definition)
	if len(in.Preview) > automation.MaxPreviewBytes {
		issues = sortIssues(append(issues, automation.Issue{Path: "preview", Code: "too_long"}))
	}
	return def, issues
}

func sortIssues(issues []automation.Issue) []automation.Issue {
	slices.SortFunc(issues, func(x, y automation.Issue) int {
		if c := strings.Compare(x.Path, y.Path); c != 0 {
			return c
		}
		return strings.Compare(x.Code, y.Code)
	})
	return slices.Compact(issues)
}

type automationDraft struct {
	userID    uint64
	in        AutomationInput
	def       automation.Definition
	canonical string
}

func writeAutomation(ctx context.Context, tx *ent.Tx, draft automationDraft) (*ent.Automation, error) {
	var existing *ent.Automation
	if draft.in.ID != "" {
		var err error
		if existing, err = loadOwnedAutomation(ctx, tx, draft.userID, draft.in); err != nil {
			return nil, err
		}
	}
	if err := checkAutomationRules(ctx, tx.Automation, draft, capCheckFor(existing, draft.in)); err != nil {
		return nil, err
	}
	canonical, err := codec.Marshal(draft.def)
	if err != nil {
		return nil, err
	}
	draft.canonical = string(canonical)
	if existing == nil {
		return createAutomation(ctx, tx, draft)
	}
	return updateAutomation(ctx, tx, existing, draft)
}

func loadOwnedAutomation(ctx context.Context, tx *ent.Tx, userID uint64, in AutomationInput) (*ent.Automation, error) {
	row, err := tx.Automation.Query().
		Where(entautomation.UserID(userID), entautomation.ID(in.ID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrAutomationNotFound
	}
	if err != nil {
		return nil, err
	}
	if in.ExpectedRevision != nil && *in.ExpectedRevision != row.Revision {
		return nil, ErrRevisionConflict
	}
	return row, nil
}

func createAutomation(ctx context.Context, tx *ent.Tx, draft automationDraft) (*ent.Automation, error) {
	create := tx.Automation.Create().
		SetID(uuid.NewString()).
		SetUserID(draft.userID).
		SetName(draft.in.Name).
		SetNameKey(strings.ToLower(draft.in.Name)).
		SetEnabled(draft.in.Enabled).
		SetRevision(1).
		SetTriggerType(draft.def.Trigger.Type).
		SetDefinition(draft.canonical)
	if draft.in.Preview != "" {
		create.SetPreview(draft.in.Preview)
	}
	return create.Save(ctx)
}

func updateAutomation(ctx context.Context, tx *ent.Tx, row *ent.Automation, draft automationDraft) (*ent.Automation, error) {
	write := tx.Automation.Update().
		Where(entautomation.ID(row.ID), entautomation.Revision(row.Revision)).
		SetName(draft.in.Name).
		SetNameKey(strings.ToLower(draft.in.Name)).
		SetEnabled(draft.in.Enabled).
		SetRevision(row.Revision + 1).
		SetTriggerType(draft.def.Trigger.Type).
		SetDefinition(draft.canonical)
	if draft.in.Preview != "" {
		write.SetPreview(draft.in.Preview)
	} else {
		write.ClearPreview()
	}
	affected, err := write.Save(ctx)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrRevisionConflict
	}
	return tx.Automation.Get(ctx, row.ID)
}

func mapAutomationConstraint(err error) error {
	if ent.IsConstraintError(err) {
		return &AutomationRefusal{Code: rpc.CodeConflict, Issues: []automation.Issue{{Path: "name", Code: "duplicate"}}}
	}
	return err
}
