package model

import (
	"database/sql"
	"fmt"

	"github.com/ViBiOh/httputils/db"
)

const fundByIsinQuery = `
SELECT
  label,
  score
FROM
  funds
WHERE
  isin = $1
`

const fundsWithScoreAboveQuery = `
SELECT
  isin,
  label,
  score
FROM
  funds
WHERE
  score >= $1
ORDER BY
  isin ASC
`

const fundsCreateQuery = `
INSERT INTO
  funds
(
  isin,
  label,
  score
) VALUES (
  $1,
  $2,
  $3
)`

const fundsUpdateScoreQuery = `
UPDATE
  funds
SET
  score = $1,
  update_date = $2
WHERE
  isin = $3
`

func scanFunds(rows *sql.Rows, pageSize uint) ([]*Fund, error) {
	var (
		isin  string
		label string
		score float64
	)

	list := make([]*Fund, 0, pageSize)

	for rows.Next() {
		if err := rows.Scan(&isin, &label, &score); err != nil {
			return nil, fmt.Errorf(`Error while scanning fund line: %v`, err)
		}

		list = append(list, &Fund{Isin: isin, Label: label, Score: score})
	}

	return list, nil
}

// ReadFundByIsin retrieves Fund by isin
func ReadFundByIsin(isin string) (*Fund, error) {
	var (
		label string
		score float64
	)

	err := fundsDB.QueryRow(fundByIsinQuery, isin).Scan(&label, &score)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf(`Error while querying: %v`, err)
	}

	return &Fund{Isin: isin, Label: label, Score: score}, nil
}

// ListFundsWithScoreAbove retrieves Fund with score above given level
func ListFundsWithScoreAbove(minScore float64) (funds []*Fund, err error) {
	rows, err := fundsDB.Query(fundsWithScoreAboveQuery, minScore)
	if err != nil {
		err = fmt.Errorf(`Error while querying: %v`, err)
		return
	}

	defer func() {
		err = db.RowsClose(rows, err)
	}()

	return scanFunds(rows, 0)
}

// SaveFund saves Fund
func SaveFund(fund *Fund, tx *sql.Tx) (err error) {
	if fund == nil {
		return fmt.Errorf(`Unable to save nil Fund`)
	}

	var usedTx *sql.Tx
	if usedTx, err = db.GetTx(fundsDB, tx); err != nil {
		return
	}

	if usedTx != tx {
		defer func() {
			err = db.EndTx(usedTx, err)
		}()
	}

	if _, err = ReadFundByIsin(fund.Isin); err != nil {
		if err == sql.ErrNoRows {
			if _, err = tx.Exec(fundsCreateQuery, fund.Isin, fund.Label, fund.Score); err != nil {
				err = fmt.Errorf(`Error while creating: %v`, err)
			}
		} else {
			err = fmt.Errorf(`Error while checking if fund already exists: %v`, err)
		}
	} else if _, err = tx.Exec(fundsUpdateScoreQuery, fund.Score, `now()`, fund.Isin); err != nil {
		err = fmt.Errorf(`Error while updating: %v`, err)
	}

	return
}
