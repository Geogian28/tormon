package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

// --- Data Structures ---

type TicketQueueItem struct {
	ID          int
	DisplayID   string
	Status      string
	MachineName string
	PackageName string
	CreatedAt   string
}

type Event struct {
	AuthorType string
	AuthorName string
	Message    string
	CreatedAt  string
}

type TicketPageData struct {
	Ticket        TicketQueueItem
	VMID          int
	Events        []Event
	AffectedHosts []string
	StillAffected []string
}
type FailureReport struct {
	Hostname    string `json:"hostname"`
	PackageName string `json:"package_name"`
	Message     string `json:"message"`
}

var serverName = "https://tormon.brohome.net"

func main() {
	var testMode bool
	flag.BoolVar(&testMode, "dev", false, "Run in development mode")
	flag.Parse()
	if testMode {
		serverName = "http://tormon-dev.brohome.net"
		fmt.Println("Running in dev mode (https://tormon-dev.brohome.net)")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./tormon.db"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// NOTICE: We removed the tmplQueue and tmplDetail from up here!

	// --- THE SINGLE UNIFIED ROUTE ---
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		// SCENARIO A: The user is at the exact root (Queue)
		if r.URL.Path == "/" {

			// 1. Read all templates in the folder so it knows what "head" is
			tmplQueue, err := template.ParseGlob("templates/*.html")
			if err != nil {
				http.Error(w, "Error loading template: "+err.Error(), 500)
				return
			}

			rows, err := db.Query(`
				SELECT 
					t.id, '#ASM-' || printf('%04d', t.id), t.status, 
					m.hostname, p.name, time(t.created_at)
				FROM tickets t
				JOIN machines m ON t.machine_id = m.id
				JOIN packages p ON t.package_id = p.id
				ORDER BY t.created_at DESC;
			`)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			defer rows.Close()

			var queue []TicketQueueItem
			for rows.Next() {
				var t TicketQueueItem
				rows.Scan(&t.ID, &t.DisplayID, &t.Status, &t.MachineName, &t.PackageName, &t.CreatedAt)
				queue = append(queue, t)
			}

			// 2. Execute the specific template by name and catch any errors
			err = tmplQueue.ExecuteTemplate(w, "tormon.html", queue)
			if err != nil {
				http.Error(w, "Error rendering page: "+err.Error(), 500)
			}
			return
		}

		// SCENARIO B: The user is looking for a specific ticket
		ticketID := strings.TrimPrefix(r.URL.Path, "/")

		// 1. Read the detail HTML file fresh on every single page load
		tmplDetail, err := template.ParseGlob("templates/*.html")
		if err != nil {
			http.Error(w, "Error loading template: "+err.Error(), 500)
			return
		}

		var data TicketPageData

		err = db.QueryRow(`
			SELECT 
				t.id, '#ASM-' || printf('%04d', t.id), t.status, 
				m.hostname, m.vmid, p.name
			FROM tickets t
			JOIN machines m ON t.machine_id = m.id
			JOIN packages p ON t.package_id = p.id
			WHERE t.id = ?`, ticketID).Scan(
			&data.Ticket.ID, &data.Ticket.DisplayID, &data.Ticket.Status,
			&data.Ticket.MachineName, &data.VMID, &data.Ticket.PackageName,
		)
		if err != nil {
			http.Error(w, "Ticket not found", 404)
			return
		}

		eventRows, err := db.Query(`
			SELECT author_type, author_name, message, time(created_at)
			FROM ticket_events
			WHERE ticket_id = ?
			ORDER BY created_at ASC`, ticketID)
		if err == nil {
			defer eventRows.Close()
			for eventRows.Next() {
				var e Event
				eventRows.Scan(&e.AuthorType, &e.AuthorName, &e.Message, &e.CreatedAt)
				data.Events = append(data.Events, e)
			}
		}

		// --- NEW: Parse Affected & Still Affected Hosts ---
		hostMap := make(map[string]bool)
		stillAffectedMap := make(map[string]bool)

		// 1. Add Patient Zero
		hostMap[data.Ticket.MachineName] = true
		stillAffectedMap[data.Ticket.MachineName] = true
		data.AffectedHosts = append(data.AffectedHosts, data.Ticket.MachineName)

		// 2. Scrape timeline for failures and resolutions
		for _, e := range data.Events {
			if strings.HasPrefix(e.Message, "[Also failed on ") {
				endIdx := strings.Index(e.Message, "]")
				if endIdx > -1 {
					host := strings.TrimPrefix(e.Message[:endIdx], "[Also failed on ")
					if !hostMap[host] {
						hostMap[host] = true
						data.AffectedHosts = append(data.AffectedHosts, host)
					}
					stillAffectedMap[host] = true // Mark as actively failing
				}
			} else if strings.HasPrefix(e.Message, "[Resolved on ") {
				endIdx := strings.Index(e.Message, "]")
				if endIdx > -1 {
					host := strings.TrimPrefix(e.Message[:endIdx], "[Resolved on ")
					stillAffectedMap[host] = false // Remove from active failures
				}
			}
		}

		// 3. Populate the StillAffected slice
		for host, isAffected := range stillAffectedMap {
			if isAffected {
				data.StillAffected = append(data.StillAffected, host)
			}
		}
		// --- END NEW ---

		tmplDetail.ExecuteTemplate(w, "ticket_detail.html", data)
	})

	// --- ROUTE: Handle Form Submissions ---
	http.HandleFunc("/update", func(w http.ResponseWriter, r *http.Request) {
		// 1. Ensure this is a POST request (someone clicking "Submit")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}

		// 2. Parse the form data from the HTML
		r.ParseForm()
		ticketID := r.FormValue("ticket_id")
		message := r.FormValue("message")

		if ticketID == "" || message == "" {
			http.Error(w, "Missing data", 400)
			return
		}

		// 3. Insert the new comment into the database!
		// We are hardcoding the author as "sam" and "operator" for the UI
		_, err := db.Exec(`
			INSERT INTO ticket_events (ticket_id, author_type, author_name, message) 
			VALUES (?, 'operator', 'sam', ?)`,
			ticketID, message)

		if err != nil {
			http.Error(w, "Database error: "+err.Error(), 500)
			return
		}

		// 4. (Optional but good practice) Update the 'updated_at' time on the main ticket
		db.Exec(`UPDATE tickets SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, ticketID)

		// 5. Instantly redirect the user back to the ticket they were just on
		http.Redirect(w, r, "/"+ticketID+"#update-form", http.StatusSeeOther)
	})

	// --- ROUTE: Smart API for Assimilator Daemons ---
	http.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST allowed", 405)
			return
		}

		// 1. Decode the JSON sent by the Assimilator daemon
		var report FailureReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			http.Error(w, "Bad JSON format", 400)
			return
		}

		// 2. Look up the internal IDs for the Machine and Package
		var machineID int
		err := db.QueryRow("SELECT id FROM machines WHERE hostname = ?", report.Hostname).Scan(&machineID)
		if err != nil {
			res, _ := db.Exec("INSERT INTO machines (hostname, vmid) VALUES (?, 0)", report.Hostname)
			newID, _ := res.LastInsertId()
			machineID = int(newID)
		}

		var packageID int
		err = db.QueryRow("SELECT id FROM packages WHERE name = ?", report.PackageName).Scan(&packageID)
		if err != nil {
			// Package doesn't exist, so we create it instantly
			res, _ := db.Exec("INSERT INTO packages (name) VALUES (?)", report.PackageName)
			newID, _ := res.LastInsertId()
			packageID = int(newID)
		}

		// 3. THE DEDUPLICATION LOGIC: Is there an OPEN or PENDING ticket?
		var existingTicketID int
		var existingStatus string

		// Notice we now check for 'open' OR 'pending'
		err = db.QueryRow(`
			SELECT id, status FROM tickets 
			WHERE package_id = ? AND status IN ('open', 'pending') 
			ORDER BY created_at DESC LIMIT 1`, packageID).Scan(&existingTicketID, &existingStatus)

		if err == sql.ErrNoRows {
			// SCENARIO A: No open/pending ticket found. We are Patient Zero. Create a new ticket!
			res, _ := db.Exec("INSERT INTO tickets (machine_id, package_id, status) VALUES (?, ?, 'open')", machineID, packageID)
			newID, _ := res.LastInsertId()
			existingTicketID = int(newID)

			// Log the initial failure
			db.Exec(`INSERT INTO ticket_events (ticket_id, author_type, author_name, message) 
					 VALUES (?, 'system', 'Assimilator', ?)`, existingTicketID, report.Message)

			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, "Created new ticket #%d", existingTicketID)

		} else {
			// SCENARIO B: Ticket already exists! Add a comment instead.
			groupedMessage := fmt.Sprintf("[Also failed on %s] %s", report.Hostname, report.Message)

			db.Exec(`INSERT INTO ticket_events (ticket_id, author_type, author_name, message) 
					 VALUES (?, 'system', 'Assimilator', ?)`, existingTicketID, groupedMessage)

			// --- THE NEW RETRY LOGIC ---
			if existingStatus == "pending" {
				// Assimilator failed the retry! Flip it back to 'open' and bump the timestamp
				db.Exec("UPDATE tickets SET status = 'open', updated_at = CURRENT_TIMESTAMP WHERE id = ?", existingTicketID)

				// Add an event showing Assimilator changed the status back
				db.Exec(`INSERT INTO ticket_events (ticket_id, author_type, author_name, message) 
						 VALUES (?, 'status_change', 'Assimilator', 'open')`, existingTicketID)
			} else {
				// It was already open, just bump the updated_at timestamp
				db.Exec("UPDATE tickets SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", existingTicketID)
			}

			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Appended to existing ticket #%d", existingTicketID)
		}
	})

	// --- ROUTE: Update Ticket Status from UI ---
	http.HandleFunc("/api/ticket/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST allowed", 405)
			return
		}

		var req struct {
			TicketID int    `json:"ticket_id"`
			Status   string `json:"status"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", 400)
			return
		}

		// --- NEW: Check if the status is actually changing! ---
		var currentStatus string
		err := db.QueryRow("SELECT status FROM tickets WHERE id = ?", req.TicketID).Scan(&currentStatus)
		if err == nil && currentStatus == req.Status {
			// It's already this status, just return OK and skip database writes
			w.WriteHeader(http.StatusOK)
			return
		}

		// Update the database AND bump the updated_at timestamp so it jumps to the top of the queue
		_, err = db.Exec("UPDATE tickets SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", req.Status, req.TicketID)
		if err != nil {
			http.Error(w, "Database error", 500)
			return
		}

		db.Exec(`INSERT INTO ticket_events (ticket_id, author_type, author_name, message) 
				 VALUES (?, 'status_change', 'sam', ?)`, req.TicketID, req.Status)

		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Only GET allowed", 405)
			return
		}

		hostname := r.URL.Query().Get("hostname")
		packageName := r.URL.Query().Get("package_name")

		if hostname == "" || packageName == "" {
			http.Error(w, "Missing parameters", 400)
			return
		}

		// Look up the most recent ticket for this exact machine and package combination
		var status string
		var ticketID int
		err := db.QueryRow(`
			SELECT t.status, t.id
			FROM tickets t
			JOIN machines m ON t.machine_id = m.id
			JOIN packages p ON t.package_id = p.id
			WHERE m.hostname = ? AND p.name = ?
			ORDER BY t.created_at DESC LIMIT 1
		`, hostname, packageName).Scan(&status, &ticketID)

		w.Header().Set("Content-Type", "application/json")

		// If no ticket is found, return "none"
		if err == sql.ErrNoRows {
			json.NewEncoder(w).Encode(map[string]string{
				"status":    "none",
				"ticket_id": "0",
			})
			return
		} else if err != nil {
			http.Error(w, "Database error", 500)
			return
		}

		// Return the actual status (open, pending, closed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    status,
			"ticket_id": ticketID,
		})
	})

	// --- ROUTE: Background Polling for UI Updates ---
	http.HandleFunc("/api/ticket/poll", func(w http.ResponseWriter, r *http.Request) {
		ticketID := r.URL.Query().Get("id")
		offset := r.URL.Query().Get("offset") // How many events the UI currently has

		// 1. Fetch any events that happened AFTER the UI's offset
		eventRows, _ := db.Query(`
			SELECT author_type, author_name, message, time(created_at)
			FROM ticket_events WHERE ticket_id = ? ORDER BY created_at ASC LIMIT -1 OFFSET ?`,
			ticketID, offset)

		var newEvents []Event
		defer eventRows.Close()
		for eventRows.Next() {
			var e Event
			eventRows.Scan(&e.AuthorType, &e.AuthorName, &e.Message, &e.CreatedAt)
			newEvents = append(newEvents, e)
		}

		// 2. Quickly recalculate Still Affected (requires full timeline)
		var machineName string
		db.QueryRow(`SELECT m.hostname FROM tickets t JOIN machines m ON t.machine_id = m.id WHERE t.id = ?`, ticketID).Scan(&machineName)

		stillMap := make(map[string]bool)
		stillMap[machineName] = true

		allRows, _ := db.Query(`SELECT message FROM ticket_events WHERE ticket_id = ? ORDER BY created_at ASC`, ticketID)
		defer allRows.Close()
		for allRows.Next() {
			var msg string
			allRows.Scan(&msg)
			if strings.HasPrefix(msg, "[Also failed on ") {
				host := strings.TrimPrefix(msg[:strings.Index(msg, "]")], "[Also failed on ")
				stillMap[host] = true
			} else if strings.HasPrefix(msg, "[Resolved on ") {
				host := strings.TrimPrefix(msg[:strings.Index(msg, "]")], "[Resolved on ")
				stillMap[host] = false
			}
		}

		var stillAffected []string
		for host, isAffected := range stillMap {
			if isAffected {
				stillAffected = append(stillAffected, host)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"new_events":     newEvents,
			"still_affected": stillAffected,
		})
	})

	// --- ROUTE: Assimilator Success Reporting ---
	http.HandleFunc("/api/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var report FailureReport
		json.NewDecoder(r.Body).Decode(&report)

		var ticketID int
		err := db.QueryRow(`
			SELECT t.id FROM tickets t JOIN packages p ON t.package_id = p.id 
			WHERE p.name = ? AND t.status IN ('open', 'pending') ORDER BY t.created_at DESC LIMIT 1`,
			report.PackageName).Scan(&ticketID)

		if err == nil {
			msg := fmt.Sprintf("[Resolved on %s] %s", report.Hostname, report.Message)
			db.Exec(`INSERT INTO ticket_events (ticket_id, author_type, author_name, message) VALUES (?, 'system', 'Assimilator', ?)`, ticketID, msg)
			db.Exec("UPDATE tickets SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", ticketID)
		}
	})

	fmt.Println("Tormon running on port 8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
