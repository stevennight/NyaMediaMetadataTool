package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var ErrTaskNotFound = errors.New("task not found")

type Task struct {
	ID                int64  `json:"id"`
	MediaFileID       *int64 `json:"mediaFileId"`
	MediaPath         string `json:"mediaPath"`
	Type              string `json:"type"`
	Status            string `json:"status"`
	OverwriteExisting bool   `json:"overwriteExisting"`
	Attempts          int    `json:"attempts"`
	ErrorSummary      string `json:"errorSummary"`
	StartedAt         string `json:"startedAt"`
	FinishedAt        string `json:"finishedAt"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

type TaskLog struct {
	ID        int64  `json:"id"`
	TaskID    int64  `json:"taskId"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"createdAt"`
}

type TaskDetail struct {
	Task      Task       `json:"task"`
	Logs      []TaskLog  `json:"logs"`
	Artifacts []Artifact `json:"artifacts"`
}

type TaskListFilters struct {
	Page     int
	PageSize int
	Path     string
	Status   string
	From     string
	To       string
}

type TaskListResult struct {
	Items    []Task `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

func (s *Store) ListTasks(ctx context.Context, limit int) ([]Task, error) {
	result, err := s.ListTasksFiltered(ctx, TaskListFilters{Page: 1, PageSize: limit})
	return result.Items, err
}

func (s *Store) ListTasksFiltered(ctx context.Context, filters TaskListFilters) (TaskListResult, error) {
	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.PageSize <= 0 || filters.PageSize > 200 {
		filters.PageSize = 50
	}

	where, args := buildTaskWhere(filters)

	var total int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM tasks
LEFT JOIN media_files ON media_files.id = tasks.media_file_id
`+where, args...).Scan(&total)
	if err != nil {
		return TaskListResult{}, err
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, filters.PageSize, (filters.Page-1)*filters.PageSize)

	rows, err := s.db.QueryContext(ctx, `
SELECT tasks.id, tasks.media_file_id, COALESCE(media_files.path, ''), tasks.type, tasks.status, tasks.overwrite_existing, tasks.attempts, tasks.error_summary,
       COALESCE(tasks.started_at, ''), COALESCE(tasks.finished_at, ''), tasks.created_at, tasks.updated_at
FROM tasks
LEFT JOIN media_files ON media_files.id = tasks.media_file_id
`+where+`
ORDER BY tasks.id DESC
LIMIT ?
OFFSET ?
`, queryArgs...)
	if err != nil {
		return TaskListResult{}, err
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		var mediaFileID *int64
		if err := rows.Scan(
			&task.ID,
			&mediaFileID,
			&task.MediaPath,
			&task.Type,
			&task.Status,
			&task.OverwriteExisting,
			&task.Attempts,
			&task.ErrorSummary,
			&task.StartedAt,
			&task.FinishedAt,
			&task.CreatedAt,
			&task.UpdatedAt,
		); err != nil {
			return TaskListResult{}, err
		}
		task.MediaFileID = mediaFileID
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return TaskListResult{}, err
	}
	return TaskListResult{Items: tasks, Total: total, Page: filters.Page, PageSize: filters.PageSize}, nil
}

func buildTaskWhere(filters TaskListFilters) (string, []any) {
	clauses := make([]string, 0)
	args := make([]any, 0)

	if path := strings.TrimSpace(filters.Path); path != "" {
		clauses = append(clauses, "media_files.path LIKE ?")
		args = append(args, "%"+path+"%")
	}
	if status := strings.TrimSpace(filters.Status); status != "" && status != "all" {
		clauses = append(clauses, "tasks.status = ?")
		args = append(args, status)
	}
	if from := strings.TrimSpace(filters.From); from != "" {
		clauses = append(clauses, "tasks.created_at >= ?")
		args = append(args, normalizeTaskTime(from, false))
	}
	if to := strings.TrimSpace(filters.To); to != "" {
		clauses = append(clauses, "tasks.created_at <= ?")
		args = append(args, normalizeTaskTime(to, true))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func normalizeTaskTime(value string, endOfDay bool) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "T", " ")
	if len(value) == len("2006-01-02") {
		if endOfDay {
			return value + " 23:59:59"
		}
		return value + " 00:00:00"
	}
	if len(value) == len("2006-01-02 15:04") {
		return value + ":00"
	}
	return value
}

func (s *Store) GetTaskDetail(ctx context.Context, id int64) (TaskDetail, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	logs, err := s.ListTaskLogs(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	artifacts, err := s.ListArtifactsByTask(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	return TaskDetail{Task: task, Logs: logs, Artifacts: artifacts}, nil
}

func (s *Store) GetTask(ctx context.Context, id int64) (Task, error) {
	var task Task
	var mediaFileID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT tasks.id, tasks.media_file_id, COALESCE(media_files.path, ''), tasks.type, tasks.status, tasks.overwrite_existing, tasks.attempts, tasks.error_summary,
       COALESCE(tasks.started_at, ''), COALESCE(tasks.finished_at, ''), tasks.created_at, tasks.updated_at
FROM tasks
LEFT JOIN media_files ON media_files.id = tasks.media_file_id
WHERE tasks.id = ?
`, id).Scan(
		&task.ID,
		&mediaFileID,
		&task.MediaPath,
		&task.Type,
		&task.Status,
		&task.OverwriteExisting,
		&task.Attempts,
		&task.ErrorSummary,
		&task.StartedAt,
		&task.FinishedAt,
		&task.CreatedAt,
		&task.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Task{}, ErrTaskNotFound
		}
		return Task{}, err
	}
	if mediaFileID.Valid {
		task.MediaFileID = &mediaFileID.Int64
	}
	return task, nil
}

func (s *Store) AddTaskLog(ctx context.Context, taskID int64, level string, message string, detail string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO task_logs (task_id, level, message, detail)
VALUES (?, ?, ?, ?)
`, taskID, level, message, detail)
	return err
}

func (s *Store) ListTaskLogs(ctx context.Context, taskID int64) ([]TaskLog, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, task_id, level, message, detail, created_at
FROM task_logs
WHERE task_id = ?
ORDER BY id ASC
`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]TaskLog, 0)
	for rows.Next() {
		var log TaskLog
		if err := rows.Scan(&log.ID, &log.TaskID, &log.Level, &log.Message, &log.Detail, &log.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}
