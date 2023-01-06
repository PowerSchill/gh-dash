package issuessection

import (
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/dlvhdr/gh-dash/config"
	"github.com/dlvhdr/gh-dash/data"
	"github.com/dlvhdr/gh-dash/ui/components/issue"
	"github.com/dlvhdr/gh-dash/ui/components/section"
	"github.com/dlvhdr/gh-dash/ui/components/table"
	"github.com/dlvhdr/gh-dash/ui/constants"
	"github.com/dlvhdr/gh-dash/ui/context"
	"github.com/dlvhdr/gh-dash/ui/keys"
	"github.com/dlvhdr/gh-dash/utils"
)

const SectionType = "issue"

type Model struct {
	section.Model
	Issues []data.IssueData
}

func NewModel(id int, ctx *context.ProgramContext, cfg config.IssuesSectionConfig, lastUpdated time.Time) Model {
	m := Model{
		section.NewModel(
			id,
			ctx,
			cfg.ToSectionConfig(),
			SectionType,
			GetSectionColumns(cfg, ctx),
			"Issue",
			"Issues",
			lastUpdated,
		),
		[]data.IssueData{},
	}

	return m
}

func (m Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.KeyMsg:

		if m.IsSearchFocused() {
			switch {

			case msg.Type == tea.KeyCtrlC, msg.Type == tea.KeyEsc:
				m.SearchBar.SetValue(m.SearchValue)
				blinkCmd := m.SetIsSearching(false)
				return &m, blinkCmd

			case msg.Type == tea.KeyEnter:
				m.SearchValue = m.SearchBar.Value()
				m.SetIsSearching(false)
				return &m, tea.Batch(m.FetchSectionRows()...)
			}

			break
		}

		switch {
		case key.Matches(msg, keys.IssueKeys.Close):
			cmd = m.close()

		case key.Matches(msg, keys.IssueKeys.Reopen):
			cmd = m.reopen()

		}

	case UpdateIssueMsg:
		for i, currIssue := range m.Issues {
			if currIssue.Number == msg.IssueNumber {
				if msg.IsClosed != nil {
					if *msg.IsClosed == true {
						currIssue.State = "CLOSED"
					} else {
						currIssue.State = "OPEN"
					}
				}
				if msg.NewComment != nil {
					currIssue.Comments.Nodes = append(currIssue.Comments.Nodes, *msg.NewComment)
				}
				m.Issues[i] = currIssue
				m.Table.SetRows(m.BuildRows())
				break
			}
		}

	case SectionIssuesFetchedMsg:
		m.Issues = msg.Issues
		m.Table.SetRows(m.BuildRows())
		m.UpdateLastUpdated(time.Now())

	}

	search, searchCmd := m.SearchBar.Update(msg)
	m.SearchBar = search
	return &m, tea.Batch(cmd, searchCmd)
}

func GetSectionColumns(cfg config.IssuesSectionConfig, ctx *context.ProgramContext) []table.Column {
	dLayout := ctx.Config.Defaults.Layout.Issues
	sLayout := cfg.Layout

	updatedAtLayout := config.MergeColumnConfigs(dLayout.UpdatedAt, sLayout.UpdatedAt)
	stateLayout := config.MergeColumnConfigs(dLayout.State, sLayout.State)
	repoLayout := config.MergeColumnConfigs(dLayout.Repo, sLayout.Repo)
	titleLayout := config.MergeColumnConfigs(dLayout.Title, sLayout.Title)
	creatorLayout := config.MergeColumnConfigs(dLayout.Creator, sLayout.Creator)
	assigneesLayout := config.MergeColumnConfigs(dLayout.Assignees, sLayout.Assignees)
	commentsLayout := config.MergeColumnConfigs(dLayout.Comments, sLayout.Comments)
	reactionsLayout := config.MergeColumnConfigs(dLayout.Reactions, sLayout.Reactions)

	return []table.Column{
		{
			Title:  "",
			Width:  updatedAtLayout.Width,
			Hidden: updatedAtLayout.Hidden,
		},
		{
			Title:  "",
			Width:  stateLayout.Width,
			Hidden: stateLayout.Hidden,
		},
		{
			Title:  "",
			Width:  repoLayout.Width,
			Hidden: repoLayout.Hidden,
		},
		{
			Title:  "Title",
			Grow:   utils.BoolPtr(true),
			Hidden: titleLayout.Hidden,
		},
		{
			Title:  "Creator",
			Width:  creatorLayout.Width,
			Hidden: creatorLayout.Hidden,
		},
		{
			Title:  "Assignees",
			Width:  assigneesLayout.Width,
			Hidden: assigneesLayout.Hidden,
		},
		{
			Title:  "",
			Width:  &issueNumCommentsCellWidth,
			Hidden: commentsLayout.Hidden,
		},
		{
			Title:  "",
			Width:  &issueNumCommentsCellWidth,
			Hidden: reactionsLayout.Hidden,
		},
	}
}

func (m *Model) BuildRows() []table.Row {
	var rows []table.Row
	for _, currIssue := range m.Issues {
		issueModel := issue.Issue{Ctx: m.Ctx, Data: currIssue}
		rows = append(rows, issueModel.ToTableRow())
	}

	if rows == nil {
		rows = []table.Row{}
	}

	return rows
}

func (m *Model) NumRows() int {
	return len(m.Issues)
}

type SectionIssuesFetchedMsg struct {
	SectionId int
	Issues    []data.IssueData
	Err       error
}

func (msg SectionIssuesFetchedMsg) GetSectionId() int {
	return msg.SectionId
}

func (msg SectionIssuesFetchedMsg) GetSectionType() string {
	return SectionType
}

func (m *Model) GetCurrRow() data.RowData {
	if len(m.Issues) == 0 {
		return nil
	}
	issue := m.Issues[m.Table.GetCurrItem()]
	return &issue
}

func (m *Model) FetchSectionRows() []tea.Cmd {
	if m == nil {
		return nil
	}
	m.Issues = nil
	m.Table.ResetCurrItem()
	m.Table.Rows = nil
	var cmds []tea.Cmd

	taskId := fmt.Sprintf("fetching_issues_%d", m.Id)
	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf(`Fetching issues for "%s"`, m.Config.Title),
		FinishedText: fmt.Sprintf(`Issues for "%s" have been fetched`, m.Config.Title),
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.Ctx.StartTask(task)
	cmds = append(cmds, startCmd)

	fetchCmd := func() tea.Msg {
		limit := m.Config.Limit
		if limit == nil {
			limit = &m.Ctx.Config.Defaults.IssuesLimit
		}
		fetchedIssues, err := data.FetchIssues(m.GetFilters(), *limit)
		if err != nil {
			return constants.TaskFinishedMsg{
				SectionId:   m.Id,
				SectionType: m.Type,
				TaskId:      taskId,
				Err:         err,
			}
		}

		sort.Slice(fetchedIssues, func(i, j int) bool {
			return fetchedIssues[i].UpdatedAt.After(fetchedIssues[j].UpdatedAt)
		})

		return constants.TaskFinishedMsg{
			SectionId:   m.Id,
			SectionType: m.Type,
			TaskId:      taskId,
			Msg: SectionIssuesFetchedMsg{
				Issues: fetchedIssues,
			},
		}

	}
	cmds = append(cmds, fetchCmd)

	return cmds
}

func (m *Model) UpdateLastUpdated(t time.Time) {
	m.Table.UpdateLastUpdated(t)
}

func FetchAllSections(ctx context.ProgramContext) (sections []section.Section, fetchAllCmd tea.Cmd) {
	sectionConfigs := ctx.Config.IssuesSections
	fetchIssuesCmds := make([]tea.Cmd, 0, len(sectionConfigs))
	sections = make([]section.Section, 0, len(sectionConfigs))
	for i, sectionConfig := range sectionConfigs {
		sectionModel := NewModel(i+1, &ctx, sectionConfig, time.Now()) // 0 is the search section
		sections = append(sections, &sectionModel)
		fetchIssuesCmds = append(fetchIssuesCmds, sectionModel.FetchSectionRows()...)
	}
	return sections, tea.Batch(fetchIssuesCmds...)
}

type UpdateIssueMsg struct {
	IssueNumber int
	NewComment  *data.Comment
	IsClosed    *bool
}
