package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func init() {

	err := godotenv.Load(".env")

	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Initialize SQLite database
	d, err := sql.Open("sqlite3", "./leaderboard.db")
	if err != nil {
		log.Fatal("Failed to open SQLite database:", err)
	}
	db = d
	// Create users table: stores tracked Codeforces users
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		codeforces_handle TEXT UNIQUE NOT NULL,
		display_name TEXT
	)`)
	if err != nil {
		log.Fatal("Failed to create users table:", err)
	}
	// Create contests table: stores relevant Codeforces contests
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS contests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		codeforces_contest_id INTEGER UNIQUE NOT NULL,
		name TEXT,
		start_time INTEGER
	)`)
	if err != nil {
		log.Fatal("Failed to create contests table:", err)
	}
	// Create user_contest_results table: stores each user's result in each contest
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS user_contest_results (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		contest_id INTEGER NOT NULL,
		rank INTEGER,
		points INTEGER,
		last_updated INTEGER,
		FOREIGN KEY(user_id) REFERENCES users(id),
		FOREIGN KEY(contest_id) REFERENCES contests(id),
		UNIQUE(user_id, contest_id)
	)`)
	if err != nil {
		log.Fatal("Failed to create user_contest_results table:", err)
	}
	// Optional: log refreshes
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS refresh_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		last_refreshed INTEGER
	)`)
	if err != nil {
		log.Fatal("Failed to create refresh_log table:", err)
	}
}

func setupRouter() *gin.Engine {
	admin_username := os.Getenv("ADMIN_USERNAME")
	admin_password_hash := os.Getenv("ADMIN_PASSWORD")
	r := gin.Default()
	r.LoadHTMLGlob("templates/*")
	//router.LoadHTMLFiles("templates/template1.html", "templates/template2.html")
	r.GET("/index", func(c *gin.Context) {
		c.Redirect(http.StatusSeeOther, "/leaderboard")
	})
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusSeeOther, "/leaderboard")
	})
	r.GET("/admin", func(c *gin.Context) {
		cookie, err := c.Cookie("admin_logged_in")

		if err != nil || cookie != admin_password_hash {
			// Use 303 See Other for redirect, not 401
			c.Redirect(http.StatusSeeOther, "/admin_login")
			return
		}
		c.HTML(http.StatusOK, "admin.tmpl", nil)
	})
	r.GET("/admin_login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin_login.tmpl", nil)
	})
	r.GET("/logout", func(c *gin.Context) {
		c.SetCookie("admin_logged_in", "", -1, "/", "", false, true)
		c.Redirect(http.StatusSeeOther, "/admin")
	})

	r.POST("/admin", func(c *gin.Context) {
		name := c.PostForm("username")
		password := c.PostForm("password")
		hashp := sha256.Sum256([]byte(password))
		if admin_password_hash == hex.EncodeToString(hashp[:]) && admin_username == name {
			c.SetCookie("admin_logged_in", hex.EncodeToString(hashp[:]), 3600*24*2, "/", "", false, true)
			c.Redirect(http.StatusSeeOther, "/admin")
		} else {
			c.HTML(http.StatusUnauthorized, "admin_login.tmpl", gin.H{"error": "Invalid credentials"})
		}
	})

	r.GET("/admin/users", func(c *gin.Context) {
		cookie, err := c.Cookie("admin_logged_in")
		if err != nil || cookie != admin_password_hash {
			c.Redirect(http.StatusUnauthorized, "/admin")
			return
		}
		rows, err := db.Query("SELECT id, codeforces_handle, display_name FROM users")
		if err != nil {
			c.String(http.StatusInternalServerError, "DB error")
			return
		}
		defer rows.Close()
		var users []map[string]interface{}
		for rows.Next() {
			var id int
			var handle, displayName string
			rows.Scan(&id, &handle, &displayName)
			users = append(users, map[string]interface{}{"id": id, "handle": handle, "display_name": displayName})
		}
		c.HTML(http.StatusOK, "admin_users.tmpl", gin.H{"users": users})
	})
	r.POST("/admin/users/add", func(c *gin.Context) {
		cookie, err := c.Cookie("admin_logged_in")
		if err != nil || cookie != admin_password_hash {
			c.Redirect(http.StatusSeeOther, "/admin_login")
			return
		}
		handle := c.PostForm("handle")
		displayName := c.PostForm("display_name")
		resp, err := http.Get("https://codeforces.com/api/user.info?handles=" + handle)
		if err != nil || resp.StatusCode != 200 {
			c.HTML(http.StatusBadRequest, "admin.tmpl", gin.H{"Users": getUsersList(), "error": "Invalid Codeforces handle"})
			return
		}
		_, err = db.Exec("INSERT INTO users (codeforces_handle, display_name) VALUES (?, ?)", handle, displayName)
		if err != nil {
			c.HTML(http.StatusBadRequest, "admin.tmpl", gin.H{"Users": getUsersList(), "error": "Could not add user: " + err.Error()})
			return
		}
		c.Redirect(http.StatusSeeOther, "/admin")
	})
	r.POST("/admin/users/delete", func(c *gin.Context) {
		cookie, err := c.Cookie("admin_logged_in")
		if err != nil || cookie != admin_password_hash {
			c.Redirect(http.StatusUnauthorized, "/admin")
			return
		}
		id := c.PostForm("id")
		_, err = db.Exec("DELETE FROM users WHERE id = ?", id)
		if err != nil {
			c.String(http.StatusBadRequest, "Could not delete user: %v", err)
			return
		}
		c.Redirect(http.StatusSeeOther, "/admin/users")
	})
	r.GET("/admin/contests", func(c *gin.Context) {
		cookie, err := c.Cookie("admin_logged_in")
		if err != nil || cookie != admin_password_hash {
			c.Redirect(http.StatusSeeOther, "/admin_login")
			return
		}
		rows, err := db.Query("SELECT id, codeforces_contest_id, name, start_time FROM contests ORDER BY start_time DESC")
		if err != nil {
			c.String(http.StatusInternalServerError, "DB error")
			return
		}
		defer rows.Close()
		var contests []map[string]interface{}
		for rows.Next() {
			var id, cfid, startTime int
			var name string
			rows.Scan(&id, &cfid, &name, &startTime)
			contests = append(contests, map[string]interface{}{"id": id, "cfid": cfid, "name": name, "start_time": startTime})
		}
		c.HTML(http.StatusOK, "admin_contests.tmpl", gin.H{"contests": contests})
	})
	r.POST("/admin/contests/add", func(c *gin.Context) {
		cookie, err := c.Cookie("admin_logged_in")
		if err != nil || cookie != admin_password_hash {
			c.Redirect(http.StatusSeeOther, "/admin_login")
			return
		}
		cfid := c.PostForm("cfid")
		resp, err := http.Get("https://codeforces.com/api/contest.standings?contestId=" + cfid + "&from=1&count=1")
		if err != nil || resp.StatusCode != 200 {
			c.String(http.StatusBadRequest, "Could not fetch contest info from Codeforces")
			return
		}
		var apiResp struct {
			Status string `json:"status"`
			Result struct {
				Contest struct {
					Id        int    `json:"id"`
					Name      string `json:"name"`
					StartTime int64  `json:"startTimeSeconds"`
				} `json:"contest"`
			} `json:"result"`
		}
		err = json.NewDecoder(resp.Body).Decode(&apiResp)
		if err != nil || apiResp.Status != "OK" {
			c.String(http.StatusBadRequest, "Could not parse contest info from Codeforces")
			return
		}
		_, err = db.Exec("INSERT INTO contests (codeforces_contest_id, name, start_time) VALUES (?, ?, ?)", apiResp.Result.Contest.Id, apiResp.Result.Contest.Name, apiResp.Result.Contest.StartTime)
		if err != nil {
			c.String(http.StatusBadRequest, "Could not add contest: %v", err)
			return
		}
		c.Redirect(http.StatusSeeOther, "/admin/contests")
	})
	r.POST("/admin/contests/delete", func(c *gin.Context) {
		cookie, err := c.Cookie("admin_logged_in")
		if err != nil || cookie != admin_password_hash {
			c.Redirect(http.StatusSeeOther, "/admin_login")
			return
		}
		id := c.PostForm("id")
		_, err = db.Exec("DELETE FROM contests WHERE id = ?", id)
		if err != nil {
			c.String(http.StatusBadRequest, "Could not delete contest: %v", err)
			return
		}
		c.Redirect(http.StatusSeeOther, "/admin/contests")
	})
	r.POST("/admin/contests/delete_all", func(c *gin.Context) {
		cookie, err := c.Cookie("admin_logged_in")
		if err != nil || cookie != admin_password_hash {
			c.Redirect(http.StatusSeeOther, "/admin_login")
			return
		}
		_, err = db.Exec("DELETE FROM contests")
		if err != nil {
			c.String(http.StatusInternalServerError, "Could not delete all contests: %v", err)
			return
		}
		c.Redirect(http.StatusSeeOther, "/admin/contests")
	})
	r.GET("/leaderboard", func(c *gin.Context) {
		userRows, err := db.Query("SELECT id, codeforces_handle, display_name FROM users")
		if err != nil {
			c.String(http.StatusInternalServerError, "DB error")
			return
		}
		defer userRows.Close()
		var users []map[string]interface{}
		for userRows.Next() {
			var id int
			var handle, displayName string
			userRows.Scan(&id, &handle, &displayName)
			users = append(users, map[string]interface{}{"id": id, "handle": handle, "display_name": displayName})
		}
		contestRows, err := db.Query("SELECT id, codeforces_contest_id, name, start_time FROM contests ORDER BY start_time DESC")
		if err != nil {
			c.String(http.StatusInternalServerError, "DB error")
			return
		}
		defer contestRows.Close()
		var contests []map[string]interface{}
		for contestRows.Next() {
			var id, cfid, startTime int
			var name string
			contestRows.Scan(&id, &cfid, &name, &startTime)
			contests = append(contests, map[string]interface{}{"id": id, "cfid": cfid, "name": name, "start_time": startTime})
		}
		// Query results for each user in each contest
		results := make(map[int]map[int]map[string]interface{}) // user_id -> contest_id -> result
		userTotals := make(map[int]int)                         // user_id -> total points
		rows, err := db.Query("SELECT user_id, contest_id, rank, points FROM user_contest_results")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var userID, contestID, rank, points int
				rows.Scan(&userID, &contestID, &rank, &points)
				// Only sum points for contests that are currently in the DB
				contestExists := false
				for _, c := range contests {
					if c["id"].(int) == contestID {
						contestExists = true
						break
					}
				}
				if !contestExists {
					continue
				}
				if results[userID] == nil {
					results[userID] = make(map[int]map[string]interface{})
				}
				results[userID][contestID] = map[string]interface{}{"rank": rank, "points": points}
				userTotals[userID] += points
			}
		}
		// Sort users by total points descending
		type userWithTotal struct {
			User  map[string]interface{}
			Total int
		}
		var userList []userWithTotal
		for _, u := range users {
			uid := u["id"].(int)
			total := userTotals[uid]
			userList = append(userList, userWithTotal{User: u, Total: total})
		}
		sort.Slice(userList, func(i, j int) bool {
			return userList[i].Total > userList[j].Total
		})
		// Assign ranks
		rankedUsers := make([]map[string]interface{}, len(userList))
		for i, ut := range userList {
			rankedUsers[i] = ut.User
			rankedUsers[i]["rank"] = i + 1
			rankedUsers[i]["total_points"] = ut.Total
		}
		c.HTML(http.StatusOK, "leaderboard.tmpl", gin.H{
			"users":      rankedUsers,
			"contests":   contests,
			"results":    results,
			"userTotals": userTotals,
		})
	})
	r.POST("/admin/contests/fetch", func(c *gin.Context) {
		cookie, err := c.Cookie("admin_logged_in")
		if err != nil || cookie != admin_password_hash {
			c.Redirect(http.StatusSeeOther, "/admin_login")
			return
		}
		err = fetchAndStoreContests()
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to fetch contests: %v", err)
			return
		}
		c.Redirect(http.StatusSeeOther, "/admin/contests")
	})
	r.POST("/admin/refresh_results", func(c *gin.Context) {
		cookie, err := c.Cookie("admin_logged_in")
		if err != nil || cookie != admin_password_hash {
			c.Redirect(http.StatusSeeOther, "/admin_login")
			return
		}
		err = refreshAllUserContestResults()
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to refresh results: %v", err)
			return
		}
		c.Redirect(http.StatusSeeOther, "/leaderboard")
	})

	return r
}

// Fetch contests from Codeforces group and update DB
func fetchAndStoreContests() error {
	resp, err := http.Get("https://codeforces.com/api/contest.list?groupCode=KRUT7MZron")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Status string `json:"status"`
		Result []struct {
			Id        int    `json:"id"`
			Name      string `json:"name"`
			StartTime int64  `json:"startTimeSeconds"`
			Phase     string `json:"phase"`
			Type      string `json:"type"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Status != "OK" {
		return err
	}
	for _, c := range result.Result {
		if c.Phase == "FINISHED" && c.Type == "CF" {
			_, err := db.Exec("INSERT OR IGNORE INTO contests (codeforces_contest_id, name, start_time) VALUES (?, ?, ?)", c.Id, c.Name, c.StartTime)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Calculate points for a given rank
func calculatePoints(rank, total int, div string) int {
	if total == 0 || rank == 0 {
		return 0
	}
	var d float64
	switch div {
	case "Div. 2", "Div. 1":
		d = 1.0
	case "Div. 3":
		d = 0.67
	case "Div. 4":
		d = 0.33
	default:
		d = 1.0
	}
	baseParticipation := 2
	score := int(math.Max(10*d*math.Log10(float64(total+1)/float64(rank+1)), 0)) + baseParticipation
	return score
}

// Refresh all user contest results and update points
func refreshAllUserContestResults() error {
	// Get all users
	userRows, err := db.Query("SELECT id, codeforces_handle FROM users")
	if err != nil {
		return err
	}
	defer userRows.Close()
	var users []struct {
		ID     int
		Handle string
	}
	for userRows.Next() {
		var id int
		var handle string
		userRows.Scan(&id, &handle)
		users = append(users, struct {
			ID     int
			Handle string
		}{id, handle})
	}
	// Get all contests
	contestRows, err := db.Query("SELECT id, codeforces_contest_id FROM contests")
	if err != nil {
		return err
	}
	defer contestRows.Close()
	var contests []struct {
		ID   int
		CFID int
	}
	for contestRows.Next() {
		var id, cfid int
		contestRows.Scan(&id, &cfid)
		contests = append(contests, struct {
			ID   int
			CFID int
		}{id, cfid})
	}
	for _, contest := range contests {
		// Fetch standings for this contest
		resp, err := http.Get("https://codeforces.com/api/contest.standings?contestId=" +
			fmt.Sprint(contest.CFID) + "&showUnofficial=false")
		if err != nil || resp.StatusCode != 200 {
			continue // skip this contest if error
		}
		var standings struct {
			Status string `json:"status"`
			Result struct {
				Contest struct {
					Name string `json:"name"`
				} `json:"contest"`
				Rows []struct {
					Party struct {
						Members []struct {
							Handle string `json:"handle"`
						} `json:"members"`
					} `json:"party"`
					Rank int `json:"rank"`
				} `json:"rows"`
			} `json:"result"`
		}
		err = json.NewDecoder(resp.Body).Decode(&standings)
		if err != nil || standings.Status != "OK" {
			continue
		}
		total := len(standings.Result.Rows)
		// Determine division from contest name
		div := "Div. 1"
		if strings.Contains(standings.Result.Contest.Name, "Div. 2") {
			div = "Div. 2"
		} else if strings.Contains(standings.Result.Contest.Name, "Div. 3") {
			div = "Div. 3"
		} else if strings.Contains(standings.Result.Contest.Name, "Div. 4") {
			div = "Div. 4"
		}
		for _, user := range users {
			userRank := 0
			for _, row := range standings.Result.Rows {
				for _, m := range row.Party.Members {
					if m.Handle == user.Handle {
						userRank = row.Rank
						break
					}
				}
				if userRank > 0 {
					break
				}
			}
			points := 0
			if userRank > 0 {
				points = calculatePoints(userRank, total, div)
			}
			_, err = db.Exec(`INSERT OR REPLACE INTO user_contest_results (user_id, contest_id, rank, points, last_updated) VALUES (?, ?, ?, ?, strftime('%s','now'))`, user.ID, contest.ID, userRank, points)
			if err != nil {
				log.Println("Failed to update result for user", user.Handle, "contest", contest.CFID, err)
			}
		}
	}
	return nil
}

// Helper to get users for admin.tmpl
func getUsersList() []map[string]interface{} {
	rows, err := db.Query("SELECT codeforces_handle, display_name FROM users")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var users []map[string]interface{}
	for rows.Next() {
		var handle, displayName string
		rows.Scan(&handle, &displayName)
		users = append(users, map[string]interface{}{"Username": handle, "DisplayName": displayName})
	}
	return users
}

func main() {
	r := setupRouter()
	// Start periodic refresh goroutine (every 1 hour)
	go func() {
		for {
			err := refreshAllUserContestResults()
			if err != nil {
				log.Println("[Auto-Refresh] Error refreshing user contest results:", err)
			}
			time.Sleep(2 * time.Hour)
		}
	}()
	r.Run(":8080")
}
