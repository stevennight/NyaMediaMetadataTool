package store

import (
	"context"
	"database/sql"
	"errors"
)

var ErrTaskNotFound = errors.New("task not found")

type Task struct {
	ID           int64  `json:"id"`
	MediaFileID  *int64 `json:"mediaFileId"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	Attempts     int    `json:"attempts"`
	ErrorSummary string `json:"errorSummary"`
	StartedAt    string `json:"startedAt"`
	FinishedAt   string `json:"finishedAt"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
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

func (s *Store) ListTasks(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, media_file_id, type, status, attempts, error_summary,
       COALESCE(started_at, ''), COALESCE(finished_at, ''), created_at, updated_at
FROM tasks
ORDER BY id DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		var mediaFileID *int64
		if err := rows.Scan(
			&task.ID,
			&mediaFileID,
			&task.Type,
			&task.Status,
			&task.Attempts,
			&task.ErrorSummary,
			&task.StartedAt,
			&task.FinishedAt,
			&task.CreatedAt,
			&task.UpdatedAt,
		); err != nil {
			return nil, err
		}
		task.MediaFileID = mediaFileID
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
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
SELECT id, media_file_id, type, status, attempts, error_summary,
       COALESCE(started_at, ''), COALESCE(finished_at, ''), created_at, updated_at
FROM tasks
WHERE id = ?
`, id).Scan(
		&task.ID,
		&mediaFileID,
		&task.Type,
		&task.Status,
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
