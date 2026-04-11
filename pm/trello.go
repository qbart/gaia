package pm

import (
	"context"
	"fmt"

	"github.com/adlio/trello"
)

type Trello struct {
	client  *trello.Client
	boardID string
	lists   map[Status]string // status -> list ID
}

func NewTrello(apiKey, token, boardID string) *Trello {
	return &Trello{
		client:  trello.NewClient(apiKey, token),
		boardID: boardID,
		lists:   make(map[Status]string),
	}
}

func (t *Trello) Init(ctx context.Context) error {
	c := t.client.WithContext(ctx)

	board, err := c.GetBoard(t.boardID, trello.Defaults())
	if err != nil {
		return fmt.Errorf("getting board: %w", err)
	}
	board.SetClient(c)

	existing, err := board.GetLists(trello.Defaults())
	if err != nil {
		return fmt.Errorf("listing lists: %w", err)
	}

	have := make(map[string]string) // name -> ID
	for _, l := range existing {
		have[l.Name] = l.ID
	}

	for _, s := range Statuses {
		name := string(s)
		if id, ok := have[name]; ok {
			t.lists[s] = id
			continue
		}
		list, err := board.CreateList(name, trello.Arguments{"pos": "bottom"})
		if err != nil {
			return fmt.Errorf("creating list %q: %w", name, err)
		}
		t.lists[s] = list.ID
	}
	return nil
}

func (t *Trello) ListTasks(ctx context.Context, status Status) ([]*Task, error) {
	c := t.client.WithContext(ctx)

	listID, ok := t.lists[status]
	if !ok {
		return nil, fmt.Errorf("unknown status %q", status)
	}

	list, err := c.GetList(listID, trello.Defaults())
	if err != nil {
		return nil, fmt.Errorf("getting list: %w", err)
	}

	cards, err := list.GetCards(trello.Defaults())
	if err != nil {
		return nil, fmt.Errorf("listing cards: %w", err)
	}

	tasks := make([]*Task, 0, len(cards))
	for _, card := range cards {
		actions, err := card.GetActions(trello.Arguments{"filter": "commentCard"})
		if err != nil {
			return nil, fmt.Errorf("getting comments for card %d: %w", card.IDShort, err)
		}
		comments := make([]string, 0, len(actions))
		for _, a := range actions {
			if a.Data != nil && a.Data.Text != "" {
				comments = append(comments, a.Data.Text)
			}
		}
		tasks = append(tasks, &Task{
			ID:       TaskID(card.ShortLink),
			Name:     fmt.Sprintf("#%d %s", card.IDShort, card.Name),
			Body:     card.Desc,
			Status:   status,
			Comments: comments,
		})
	}
	return tasks, nil
}

func (t *Trello) CreateTask(ctx context.Context, task Task) (TaskID, error) {
	c := t.client.WithContext(ctx)

	listID, ok := t.lists[StatusTodo]
	if !ok {
		return "", fmt.Errorf("todo list not initialized")
	}

	card := &trello.Card{
		Name:   task.Name,
		Desc:   task.Body,
		IDList: listID,
	}
	if err := c.CreateCard(card, trello.Defaults()); err != nil {
		return "", fmt.Errorf("creating card: %w", err)
	}
	return TaskID(card.ShortLink), nil
}

func (t *Trello) MoveTaskTo(ctx context.Context, id TaskID, status Status) error {
	c := t.client.WithContext(ctx)

	listID, ok := t.lists[status]
	if !ok {
		return fmt.Errorf("unknown status %q", status)
	}

	card, err := c.GetCard(string(id), trello.Defaults())
	if err != nil {
		return fmt.Errorf("getting card: %w", err)
	}

	return card.MoveToList(listID, trello.Defaults())
}

func (t *Trello) CommentTask(ctx context.Context, id TaskID, body string) error {
	c := t.client.WithContext(ctx)

	card, err := c.GetCard(string(id), trello.Defaults())
	if err != nil {
		return fmt.Errorf("getting card: %w", err)
	}

	_, err = card.AddComment(body, trello.Defaults())
	return err
}
