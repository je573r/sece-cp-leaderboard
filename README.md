# SECE CP Leaderboard

A modern, mobile-friendly Codeforces group leaderboard web app built with Go (Gin) and SQLite.

## Features
- Fast, always-up-to-date leaderboard (no API calls on user visits)
- Admin dashboard for user/contest management
- Automatic periodic refresh of contest/user results (background goroutine)
- Custom scoring formula and ranking logic
- Modern dark UI, responsive design
- Minimal, consistent and secured admin panel

## Setup

1. **Clone the repo**

```bash
git clone https://github.com/je573r/sece-cp-leaderboard.git
cd sece-cp-leaderboard
```

2. **Create a `.env` file**

```
ADMIN_USERNAME=youradmin
ADMIN_PASSWORD=sha256_hash_of_password
```
- To generate the password hash:
  ```bash
  echo -n 'yourpassword' | sha256sum
  ```
  Use the hex string (without trailing dash/filename).

3. **Run the server**

```bash
go run src/main.go
```

4. **Visit**
- Leaderboard: [http://localhost:8080/](http://localhost:8080/)
- Admin: [http://localhost:8080/admin](http://localhost:8080/admin)

## Customization
- Update the Codeforces group code in `main.go` if needed.
- Update the GitHub repo links in `templates/leaderboard.tmpl` for your own repo.

## License
MIT
