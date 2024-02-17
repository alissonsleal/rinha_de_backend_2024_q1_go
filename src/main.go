package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

type ExtractResponse struct {
	AccountBalance Client        `json:"saldo"`
	Transactions   []Transaction `json:"ultimas_transacoes"`
}

func main() {
	godotenv.Load()

	fmt.Println("Starting server")
	fmt.Println("Connecting to database")
	connStr := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=5432 sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
	)
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fmt.Println("Connected to database")

	app := http.NewServeMux()

	app.HandleFunc("GET /clientes/{id}/extrato", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		parsedId, err := strconv.Atoi(id)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "Invalid id")
			return
		}

		client, err := getClientById(parsedId, db)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "Client not found")
			return
		}

		transactions, err := getLastTenTransactionsByClientId(parsedId, db)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "Internal server error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		response := ExtractResponse{
			AccountBalance: client,
			Transactions:   transactions,
		}

		jsonResponse, err := json.Marshal(&response)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "Internal server error")
			return
		}

		fmt.Fprint(w, string(jsonResponse))

	})

	port := os.Getenv("APP_PORT")

	fmt.Printf("Server running on port %s\n", port)

	http.ListenAndServe(":"+port, app)
}

type Client struct {
	Date         string `json:"data_extrato"`
	AccountLimit int    `json:"limite"`
	Balance      int    `json:"total"`
}

func getClientById(id int, db *sql.DB) (Client, error) {
	rows := db.QueryRow("SELECT * FROM clients WHERE id = $1", id)

	var accountLimit int
	var balance int
	err := rows.Scan(&id, &accountLimit, &balance)
	if err != nil {
		return Client{}, err
	}

	return Client{
		Date:         time.Now().Format(time.RFC3339Nano),
		AccountLimit: accountLimit,
		Balance:      balance,
	}, nil
}

type Transaction struct {
	Amount      int       `json:"valor"`
	Operation   string    `json:"tipo"`
	Description string    `json:"descricao"`
	CreatedAt   time.Time `json:"realizada_em"`
}

func getLastTenTransactionsByClientId(id int, db *sql.DB) ([]Transaction, error) {
	rows, err := db.Query("SELECT amount, operation, description, created_at FROM transactions WHERE client_id = $1 ORDER BY created_at DESC LIMIT 10", id)
	if err != nil {
		return nil, err
	}

	var transactions []Transaction

	for rows.Next() {
		var transaction Transaction
		err = rows.Scan(&transaction.Amount, &transaction.Operation, &transaction.Description, &transaction.CreatedAt)
		if err != nil {
			return nil, err
		}

		transactions = append(transactions, transaction)
	}

	return transactions, nil
}
