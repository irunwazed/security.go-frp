package dashboard

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/irunwazed/tunnel/pkg/util"
)

//go:embed views/*.html
var viewsFS embed.FS

var tmpl = template.Must(
	template.New("").Funcs(template.FuncMap{
		"add":   func(a, b int) int { return a + b },
		"sub":   func(a, b int) int { return a - b },
		"trunc": truncStr,
	}).ParseFS(viewsFS, "views/*.html"),
)

func truncStr(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// Dashboard adalah subsistem monitoring yang berjalan di port tersendiri.
type Dashboard struct {
	db   *db
	port int
}

// New membuat instance Dashboard. Harus dipanggil sekali saat server start.
func New(port int, dbPath string) (*Dashboard, error) {
	d, err := openDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("dashboard db: %w", err)
	}
	return &Dashboard{db: d, port: port}, nil
}

func (d *Dashboard) Close() { _ = d.db.close() }

// --- Hooks dipanggil dari handleControlConn ---

func (d *Dashboard) ClientProcess(runID, name, ip, host string) {
	if err := d.db.insertWebsite(runID, name, ip, host); err != nil {
		util.Warnf("dashboard insert website: %v", err)
	}
}

func (d *Dashboard) ClientConnect(runID string) {
	if err := d.db.connectWebsite(runID); err != nil {
		util.Warnf("dashboard connect website: %v", err)
	}
}

func (d *Dashboard) ClientDisconnect(runID string) {
	if err := d.db.disconnectWebsite(runID); err != nil {
		util.Warnf("dashboard disconnect website: %v", err)
	}
}

// Start menjalankan Fiber server. Blocking.
func (d *Dashboard) Start() error {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})

	app.Get("/", func(c *fiber.Ctx) error { return c.Redirect("/websites") })
	app.Get("/websites", d.handleWebsites)
	app.Get("/messages", d.handleMessages)
	app.Post("/api/message", d.handlePostMessage)

	util.Infof("dashboard listen di :%d", d.port)
	return app.Listen(fmt.Sprintf(":%d", d.port))
}

// --- pagination helper ---

type pagination struct {
	Page       int
	TotalPages int
	Total      int
	HasPrev    bool
	HasNext    bool
	Pages      []int
}

func makePagination(page, totalPages, total int) pagination {
	p := pagination{
		Page:       page,
		TotalPages: totalPages,
		Total:      total,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
	}
	start := page - 3
	if start < 1 {
		start = 1
	}
	end := start + 6
	if end > totalPages {
		end = totalPages
	}
	if end-start < 6 && start > 1 {
		start = end - 6
		if start < 1 {
			start = 1
		}
	}
	for i := start; i <= end; i++ {
		p.Pages = append(p.Pages, i)
	}
	return p
}

func render(c *fiber.Ctx, name string, data interface{}) error {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return c.Status(500).SendString("template error: " + err.Error())
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Send(buf.Bytes())
}

// --- Handlers ---

func (d *Dashboard) handleWebsites(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	result, err := d.db.listWebsites(page, limit)
	if err != nil {
		return c.Status(500).SendString("db error: " + err.Error())
	}

	return render(c, "websites.html", map[string]interface{}{
		"ActivePage": "websites",
		"Rows":       result.Rows,
		"Pagination": makePagination(result.Page, result.TotalPages, result.Total),
	})
}

func (d *Dashboard) handleMessages(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	result, err := d.db.listMessages(page, limit)
	if err != nil {
		return c.Status(500).SendString("db error: " + err.Error())
	}

	return render(c, "messages.html", map[string]interface{}{
		"ActivePage": "messages",
		"Rows":       result.Rows,
		"Pagination": makePagination(result.Page, result.TotalPages, result.Total),
	})
}

type msgRequest struct {
	From  string `json:"from"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (d *Dashboard) handlePostMessage(c *fiber.Ctx) error {
	var req msgRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid JSON"})
	}
	if req.Name == "" || req.Value == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "name dan value wajib diisi"})
	}

	id := util.RandomID()
	if err := d.db.insertMessage(id, req.From, req.Name, req.Value); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}

	util.Infof("message baru dari %q: name=%q", req.From, req.Name)
	return c.JSON(fiber.Map{"ok": true, "id": id})
}
