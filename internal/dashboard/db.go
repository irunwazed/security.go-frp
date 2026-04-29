package dashboard

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS websites (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id       TEXT    UNIQUE NOT NULL,
    name         TEXT    DEFAULT '',
    ip           TEXT    DEFAULT '',
    host         TEXT    DEFAULT '',
    status       TEXT    DEFAULT 'process',
    connect_at   DATETIME,
    disconnect_at DATETIME,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
    id         TEXT PRIMARY KEY,
    sender     TEXT DEFAULT '',
    name       TEXT DEFAULT '',
    value      TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

type db struct {
	conn *sql.DB
}

func openDB(path string) (*db, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	conn.SetMaxOpenConns(1) // SQLite tidak support concurrent writes
	if _, err := conn.Exec(schema); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &db{conn: conn}, nil
}

func (d *db) close() error { return d.conn.Close() }

// --- Website ---

// Website adalah satu baris tabel websites.
type Website struct {
	ID           int64
	RunID        string
	Name         string
	IP           string
	Host         string
	Status       string
	ConnectAt    string
	DisconnectAt string
	CreatedAt    string
}

func (d *db) insertWebsite(runID, name, ip, host string) error {
	_, err := d.conn.Exec(`
		INSERT INTO websites (run_id, name, ip, host, status, created_at)
		VALUES (?, ?, ?, ?, 'process', CURRENT_TIMESTAMP)
		ON CONFLICT(run_id) DO UPDATE SET
			name=excluded.name, ip=excluded.ip, host=excluded.host,
			status='process', disconnect_at=NULL
	`, runID, name, ip, host)
	return err
}

func (d *db) connectWebsite(runID string) error {
	_, err := d.conn.Exec(`
		UPDATE websites SET status='connect', connect_at=CURRENT_TIMESTAMP
		WHERE run_id=?`, runID)
	return err
}

func (d *db) disconnectWebsite(runID string) error {
	_, err := d.conn.Exec(`
		UPDATE websites SET status='disconnect', disconnect_at=CURRENT_TIMESTAMP
		WHERE run_id=?`, runID)
	return err
}

// WebsitePage hasil query list + pagination.
type WebsitePage struct {
	Rows       []Website
	Total      int
	Page       int
	TotalPages int
}

func (d *db) listWebsites(page, limit int) (*WebsitePage, error) {
	page, limit = normPage(page, limit)
	offset := (page - 1) * limit

	var total int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM websites`).Scan(&total); err != nil {
		return nil, err
	}

	rows, err := d.conn.Query(`
		SELECT id, run_id, name, ip, host, status,
		       COALESCE(connect_at,''), COALESCE(disconnect_at,''), created_at
		FROM websites ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Website
	for rows.Next() {
		var w Website
		if err := rows.Scan(&w.ID, &w.RunID, &w.Name, &w.IP, &w.Host,
			&w.Status, &w.ConnectAt, &w.DisconnectAt, &w.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, w)
	}

	return &WebsitePage{
		Rows:       list,
		Total:      total,
		Page:       page,
		TotalPages: totalPages(total, limit),
	}, nil
}

// --- Message ---

// Message adalah satu baris tabel messages.
type Message struct {
	ID        string
	Sender    string
	Name      string
	Value     string
	CreatedAt string
}

func (d *db) insertMessage(id, sender, name, value string) error {
	_, err := d.conn.Exec(`
		INSERT INTO messages (id, sender, name, value, created_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`, id, sender, name, value)
	return err
}

// MessagePage hasil query list + pagination.
type MessagePage struct {
	Rows       []Message
	Total      int
	Page       int
	TotalPages int
}

func (d *db) listMessages(page, limit int) (*MessagePage, error) {
	page, limit = normPage(page, limit)
	offset := (page - 1) * limit

	var total int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&total); err != nil {
		return nil, err
	}

	rows, err := d.conn.Query(`
		SELECT id, sender, name, value, created_at
		FROM messages ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Sender, &m.Name, &m.Value, &m.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, m)
	}

	return &MessagePage{
		Rows:       list,
		Total:      total,
		Page:       page,
		TotalPages: totalPages(total, limit),
	}, nil
}

// --- helpers ---

func normPage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return page, limit
}

func totalPages(total, limit int) int {
	if limit == 0 {
		return 1
	}
	t := total / limit
	if total%limit != 0 {
		t++
	}
	return t
}
