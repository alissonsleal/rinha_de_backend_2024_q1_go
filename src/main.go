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

type TransactionBody struct {
	Amount      int    `json:"valor"`
	Operation   string `json:"tipo"`
	Description string `json:"descricao"`
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

	db.SetMaxOpenConns(20)

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
			if err == sql.ErrNoRows {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, "Client not found")
				return
			}

			w.WriteHeader(http.StatusInternalServerError)
			fmt.Println(err)
			fmt.Fprint(w, err.Error())
			return

		}

		transactions, err := getLastTenTransactionsByClientId(parsedId, db)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Println(err)
			fmt.Fprint(w, err.Error())
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
			fmt.Println(err)
			fmt.Fprint(w, err.Error())
			return
		}

		fmt.Fprint(w, string(jsonResponse))
		return

	})

	app.HandleFunc("POST /clientes/{id}/transacoes", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		parsedId, err := strconv.Atoi(id)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "Invalid id")
			return
		}

		var transaction TransactionBody
		err = json.NewDecoder(r.Body).Decode(&transaction)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, "Invalid request body")
			return
		}

		if transaction.Operation != "d" && transaction.Operation != "c" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, "Invalid operation")
			return
		}

		if len(transaction.Description) > 10 || len(transaction.Description) < 1 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, "Invalid description")
			return
		}

		response, err := createTransaction(transaction, parsedId, db)
		if err != nil {
			if err == sql.ErrNoRows {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, "Client not found")
				return
			}

			if err.Error() == "Insufficient funds" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				fmt.Fprint(w, err.Error())
				return
			}

			w.WriteHeader(http.StatusInternalServerError)
			fmt.Println(err)
			fmt.Fprint(w, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		jsonResponse, err := json.Marshal(&response)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Println(err)
			fmt.Fprint(w, err.Error())
			return
		}

		fmt.Fprint(w, string(jsonResponse))

		return
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
	rows, err := db.Query("SELECT amount, operation, description, created_at FROM transactions WHERE client_id = $1 ORDER BY created_at DESC LIMIT 10;", id)
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

type TransactionResponse struct {
	AccountLimit int `json:"limite"`
	Balance      int `json:"saldo"`
}

func createTransaction(transaction TransactionBody, clientId int, db *sql.DB) (TransactionResponse, error) {
	tx, err := db.Begin()
	if err != nil {
		return TransactionResponse{}, err
	}
	defer tx.Rollback()

	var transactionResponse TransactionResponse

	rows := tx.QueryRow("SELECT account_limit, balance FROM clients WHERE id = $1 FOR UPDATE;", clientId)

	err = rows.Scan(&transactionResponse.AccountLimit, &transactionResponse.Balance)
	if err != nil {
		fmt.Println(err)
		tx.Rollback()
		return TransactionResponse{}, err
	}

	var newBalance int

	if transaction.Operation == "d" {
		newBalance = transactionResponse.Balance - transaction.Amount
	}

	if transaction.Operation == "c" {
		newBalance = transactionResponse.Balance + transaction.Amount
	}

	if newBalance < (transactionResponse.AccountLimit * -1) {
		tx.Rollback()
		return TransactionResponse{}, fmt.Errorf("Insufficient funds")
	}

	_, err = tx.Exec("INSERT INTO transactions (client_id, amount, operation, description) VALUES ($1, $2, $3, $4);", clientId, transaction.Amount, transaction.Operation, transaction.Description)
	if err != nil {
		tx.Rollback()
		return TransactionResponse{}, err
	}
	_, err = tx.Exec("UPDATE clients SET balance = $1 WHERE id = $2;", newBalance, clientId)
	if err != nil {
		tx.Rollback()
		return TransactionResponse{}, err
	}

	err = tx.Commit()
	if err != nil {
		return TransactionResponse{}, err
	}

	return TransactionResponse{
		AccountLimit: transactionResponse.AccountLimit,
		Balance:      newBalance,
	}, nil

}
