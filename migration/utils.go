package migration

import (
	"fmt"
	"time"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/oarkflow/bcl"
)

func init() {
	f := gofakeit.New(0)
	bcl.RegisterFunction("fake_uuid", func(args ...any) (any, error) {
		return f.UUID(), nil
	})
	bcl.RegisterFunction("fake_name", func(args ...any) (any, error) {
		return f.Name(), nil
	})
	bcl.RegisterFunction("fake_firstname", func(args ...any) (any, error) {
		return f.FirstName(), nil
	})
	bcl.RegisterFunction("fake_lastname", func(args ...any) (any, error) {
		return f.LastName(), nil
	})
	bcl.RegisterFunction("fake_email", func(args ...any) (any, error) {
		return f.Email(), nil
	})
	bcl.RegisterFunction("fake_phone", func(args ...any) (any, error) {
		return f.Phone(), nil
	})
	bcl.RegisterFunction("fake_address", func(args ...any) (any, error) {
		return f.Address().Address, nil
	})
	bcl.RegisterFunction("fake_city", func(args ...any) (any, error) {
		return f.City(), nil
	})
	bcl.RegisterFunction("fake_state", func(args ...any) (any, error) {
		return f.State(), nil
	})
	bcl.RegisterFunction("fake_zip", func(args ...any) (any, error) {
		return f.Zip(), nil
	})
	bcl.RegisterFunction("fake_country", func(args ...any) (any, error) {
		return f.Country(), nil
	})
	bcl.RegisterFunction("fake_company", func(args ...any) (any, error) {
		return f.Company(), nil
	})
	bcl.RegisterFunction("fake_jobtitle", func(args ...any) (any, error) {
		return f.JobTitle(), nil
	})
	bcl.RegisterFunction("fake_ssn", func(args ...any) (any, error) {
		return f.SSN(), nil
	})
	bcl.RegisterFunction("fake_creditcard", func(args ...any) (any, error) {
		return f.CreditCardNumber(nil), nil
	})
	bcl.RegisterFunction("fake_currency", func(args ...any) (any, error) {
		return f.CurrencyShort(), nil
	})
	bcl.RegisterFunction("fake_macaddress", func(args ...any) (any, error) {
		return f.MacAddress(), nil
	})
	bcl.RegisterFunction("fake_ipv4", func(args ...any) (any, error) {
		return f.IPv4Address(), nil
	})
	bcl.RegisterFunction("fake_ipv6", func(args ...any) (any, error) {
		return f.IPv6Address(), nil
	})
	// --- New Fake Date Functions ---
	bcl.RegisterFunction("fake_date", func(args ...any) (any, error) {
		return f.Date(), nil
	})
	bcl.RegisterFunction("fake_pastdate", func(args ...any) (any, error) {
		return f.DateRange(time.Now().AddDate(-10, 0, 0), time.Now()), nil
	})
	bcl.RegisterFunction("fake_futuredate", func(args ...any) (any, error) {
		return f.DateRange(time.Now(), time.Now().AddDate(10, 0, 0)), nil
	})
	bcl.RegisterFunction("fake_daterange", func(args ...any) (any, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("fake_daterange requires 2 arguments: start and end time (YYYY-MM-DD)")
		}
		startStr, ok1 := args[0].(string)
		endStr, ok2 := args[1].(string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("fake_daterange arguments must be strings in format YYYY-MM-DD")
		}
		start, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			return nil, err
		}
		end, err := time.Parse("2006-01-02", endStr)
		if err != nil {
			return nil, err
		}
		return f.DateRange(start, end), nil
	})
	bcl.RegisterFunction("fake_nanosecond", func(args ...any) (any, error) {
		return f.Date().Nanosecond(), nil
	})
	bcl.RegisterFunction("fake_second", func(args ...any) (any, error) {
		return f.Date().Second(), nil
	})
	bcl.RegisterFunction("fake_minute", func(args ...any) (any, error) {
		return f.Date().Minute(), nil
	})
	bcl.RegisterFunction("fake_hour", func(args ...any) (any, error) {
		return f.Date().Hour(), nil
	})
	bcl.RegisterFunction("fake_month", func(args ...any) (any, error) {
		return int(f.Date().Month()), nil
	})
	bcl.RegisterFunction("fake_monthstring", func(args ...any) (any, error) {
		return f.Date().Month().String(), nil
	})
	bcl.RegisterFunction("fake_day", func(args ...any) (any, error) {
		return f.Date().Day(), nil
	})
	bcl.RegisterFunction("fake_weekday", func(args ...any) (any, error) {
		return f.Date().Weekday().String(), nil
	})
	bcl.RegisterFunction("fake_year", func(args ...any) (any, error) {
		return f.Date().Year(), nil
	})
	bcl.RegisterFunction("fake_timezone", func(args ...any) (any, error) {
		return f.Date().Location().String(), nil
	})
	bcl.RegisterFunction("fake_timezoneabv", func(args ...any) (any, error) {
		t := f.Date()
		abbr, _ := t.Zone()
		return abbr, nil
	})
	bcl.RegisterFunction("fake_timezonefull", func(args ...any) (any, error) {
		return f.Date().Location().String(), nil
	})
	bcl.RegisterFunction("fake_timezoneoffset", func(args ...any) (any, error) {
		t := f.Date()
		_, offset := t.Zone()
		hOffset := float32(offset) / 3600.0
		return hOffset, nil
	})
	bcl.RegisterFunction("fake_timezoneregion", func(args ...any) (any, error) {
		return f.Date().Location().String(), nil
	})
}

var (
	mysqlDataTypes = map[string]string{
		"string":     "VARCHAR",
		"varchar":    "VARCHAR",
		"text":       "TEXT",
		"char":       "CHAR",
		"number":     "INT",
		"int":        "INT",
		"integer":    "INT",
		"smallint":   "SMALLINT",
		"mediumint":  "MEDIUMINT",
		"bigint":     "BIGINT",
		"float":      "FLOAT",
		"double":     "DOUBLE",
		"decimal":    "DECIMAL",
		"boolean":    "TINYINT(1)",
		"bool":       "TINYINT(1)",
		"date":       "DATE",
		"datetime":   "DATETIME",
		"time":       "TIME",
		"timestamp":  "TIMESTAMP",
		"year":       "YEAR",
		"blob":       "BLOB",
		"mediumblob": "MEDIUMBLOB",
		"longblob":   "LONGBLOB",
		"binary":     "BLOB",
		"varbinary":  "VARBINARY",
		"enum":       "ENUM", // caller must supply enum values in definition if needed
		"set":        "SET",  // caller must supply set values in definition if needed
		"json":       "JSON",
	}
	postgresDataTypes = map[string]string{
		"string":    "TEXT",
		"varchar":   "VARCHAR",
		"char":      "CHAR",
		"serial":    "SERIAL",
		"bigserial": "BIGSERIAL",
		"text":      "TEXT",
		"number":    "INTEGER",
		"integer":   "INTEGER",
		"smallint":  "SMALLINT",
		"bigint":    "BIGINT",
		"decimal":   "DECIMAL",
		"numeric":   "NUMERIC",
		"boolean":   "BOOLEAN",
		"bool":      "BOOLEAN",
		"date":      "DATE",
		"datetime":  "TIMESTAMP",
		"timestamp": "TIMESTAMP",
		"time":      "TIME",
		"float":     "REAL",
		"real":      "REAL",
		"double":    "DOUBLE PRECISION",
		"money":     "MONEY",
		"uuid":      "UUID",
		"json":      "JSON",
		"jsonb":     "JSONB",
	}
	sqliteDataTypes = map[string]string{
		"string":    "TEXT",
		"varchar":   "VARCHAR",
		"text":      "TEXT",
		"char":      "CHAR",
		"number":    "INTEGER",
		"integer":   "INTEGER",
		"smallint":  "INTEGER",
		"bigint":    "INTEGER",
		"boolean":   "BOOLEAN",
		"bool":      "BOOLEAN",
		"date":      "DATE",
		"datetime":  "DATETIME",
		"timestamp": "DATETIME",
		"time":      "TIME",
		"float":     "REAL",
		"double":    "REAL",
		"real":      "REAL",
		"decimal":   "NUMERIC",
		"numeric":   "NUMERIC",
		"blob":      "BLOB",
		"binary":    "BLOB",
		"varbinary": "BLOB",
		"json":      "TEXT",
	}
)
