package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"fmt"
	_ "io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"

	_ "github.com/mattn/go-sqlite3" // Import go-sqlite3 library
)

var totalLines = 0
var teams []string
var linetext, matchDate, series, team1, team2, result string
var division, stage string
var matchid int
var opponent string
var currentMatch int

func main() {
	//log.SetOutput(ioutil.Discard)
	log.SetOutput(os.Stderr)
	phoenixBatting := make([]string, 0)
	phoenixBowling := make([]string, 0)
	phoenixFielding := make([]string, 0)

	if len(os.Args) <= 1 {
		log.Println("Usage : " + os.Args[0] + " scorecard file.csv")
		os.Exit(1)
	} else if !(fileExists(os.Args[1])) {
		log.Println("Scorecard csv file " + os.Args[1] + " Not found on the current directory.")
		log.Println("Usage : " + os.Args[0] + " scorecard file.csv")
		os.Exit(1)
	} else if filepath.Ext(os.Args[1]) != ".csv" {
		log.Println(os.Args[1] + " is not a .csv file.")
		log.Println("Usage : " + os.Args[0] + " scorecard file.csv")
		os.Exit(1)
	} else {
		dbconn := Dbconnect()
		CreateTables(dbconn)
		currentMatch = saveMatchDetails(os.Args[1], dbconn)
		log.Println("Match Saved as Match ID := " + strconv.Itoa(currentMatch))
		opponent = getOpponent(dbconn, currentMatch)
		log.Println("Match Opponent := " + opponent)
		phoenixBowling, phoenixBatting, phoenixFielding = extractRanges(os.Args[1])

		processBatting(phoenixBatting, dbconn)
		processBowling(phoenixBowling, dbconn)
		processFielding(phoenixFielding, dbconn)
		calculatePoints(dbconn)
		replacePlayer(dbconn)
		renderFinalTable(dbconn)
		dumpPointsTableAsCSV()
	}
}

func processBatting(battingArray []string, db *sql.DB) {
	log.Println("Inserting Batting details...")
	insertBatsmenSQL := `INSERT INTO batsmen (matchid,battername,runs,balls,fours,sixers,Notout) VALUES (?,?,?,?,?,?,?)`
	statement, err := db.Prepare(insertBatsmenSQL) // Prepare statement.
	// This is good to avoid SQL injections
	if err != nil {
		log.Fatalln(err.Error())
	}

	for i := 0; i < len(battingArray); i++ {
		battingSplits := strings.Split(battingArray[i], ",")
		if battingSplits[0] != "BatsMan" {
			var ballsfaced int
			ballsfaced, _ = strconv.Atoi(battingSplits[5])

			if len(battingSplits[1]) == 0 && ballsfaced > 0 {

				// Nothing on "how Out" but more than 1 ball faced, means they are not out
				_, err = statement.Exec(matchid, battingSplits[0], battingSplits[4], battingSplits[5], battingSplits[6], battingSplits[7], 1)
				if err != nil {
					log.Fatalln(err.Error())
				}
			} else {
				// Batsman is out
				_, err = statement.Exec(matchid, battingSplits[0], battingSplits[4], battingSplits[5], battingSplits[6], battingSplits[7], 0)
				if err != nil {
					log.Fatalln(err.Error())
				}

			}

		}
	}
	log.Println("Phoenix Batting Details inserted ...")
}

func processBowling(bowlingArray []string, db *sql.DB) {
	log.Println("Inserting Bowling details...")
	insertBowlersSQL := `INSERT INTO bowlers (matchid,bowlerName,overs,Maidens,RunsGiven,Wickets,Wides,NoBalls) VALUES (?, ?, ?,?, ?, ? ,?,?)`
	statement, err := db.Prepare(insertBowlersSQL) // Prepare statement.
	// This is good to avoid SQL injections
	if err != nil {
		log.Fatalln(err.Error())
	}

	for i := 0; i < len(bowlingArray); i++ {
		bowlingSplits := strings.Split(bowlingArray[i], ",")
		if bowlingSplits[0] != "Bowler" {
			_, err = statement.Exec(matchid, bowlingSplits[0], bowlingSplits[1], bowlingSplits[2], bowlingSplits[3], bowlingSplits[4], bowlingSplits[5], bowlingSplits[6])
			if err != nil {
				log.Fatalln(err.Error())
			}
		}
	}
	log.Println("Phoenix Bowling Details inserted ...")
}

func processFielding(fieldingArray []string, db *sql.DB) {
	log.Println("Inserting Fielding details...")
	insertFieldingSQL := `INSERT INTO fielders (matchid,Batsman,wicketType,fieldername,bowlername,bowled,catches,runouts) VALUES (?,?,?,?,?,?,?,?)`
	statement, err := db.Prepare(insertFieldingSQL) // Prepare statement.
	// This is good to avoid SQL injections
	if err != nil {
		log.Fatalln(err.Error())
	}

	for i := 0; i < len(fieldingArray); i++ {
		fieldingSplits := strings.Split(fieldingArray[i], ",")
		if fieldingSplits[0] != "BatsMan" {

			// Get full PlayerName from Batsman table

			fullFielderName := getFullPlayerName(db, matchid, fieldingSplits[2])
			fullBowlerName := getFullPlayerName(db, matchid, fieldingSplits[3])

			// find the dismissal Type
			switch fieldingSplits[1] {
			case "ct":
				//find if its a caught and Bowled - if bowler == Fielder
				if fieldingSplits[2] == fieldingSplits[3] {
					// Its a Caught and Bowled
					_, err = statement.Exec(matchid, fieldingSplits[0], "Caught&Bowled", fullFielderName, fullFielderName, 0, 1, 0)
					if err != nil {
						log.Fatalln(err.Error())
					}
				} else {
					// Its a catch
					_, err = statement.Exec(matchid, fieldingSplits[0], "Caught", fullFielderName, fullBowlerName, 0, 1, 0)
					if err != nil {
						log.Fatalln(err.Error())
					}
				}
			case "b":
				_, err = statement.Exec(matchid, fieldingSplits[0], "Bowled", "", fullBowlerName, 1, 0, 0)
				if err != nil {
					log.Fatalln(err.Error())
				}
			case "ro":
				// Find if its a Direct Hit - If Filder Name is null , then its a Direct Hit
				if strings.Trim(fieldingSplits[2], " ") == "" {
					// Its a Direct Hit
					_, err = statement.Exec(matchid, fieldingSplits[0], "RunOut-DirectHit", fullBowlerName, "", 0, 0, 1)
					if err != nil {
						log.Fatalln(err.Error())
					}
				} else {
					// Simple Runout , 2 fielders are involved , give runout credit to both
					_, err = statement.Exec(matchid, fieldingSplits[0], "RunOut", fullFielderName, "", 0, 0, 1)
					if err != nil {
						log.Fatalln(err.Error())
					}
					_, err = statement.Exec(matchid, fieldingSplits[0], "RunOut", fullBowlerName, "", 0, 0, 1)
					if err != nil {
						log.Fatalln(err.Error())
					}
				}
			case "ctw":
				// Caught Behind
				_, err = statement.Exec(matchid, fieldingSplits[0], "CaughtBehind", fullFielderName, fullBowlerName, 0, 1, 0)
				if err != nil {
					log.Fatalln(err.Error())
				}
			}
		}
	}
	log.Println("Phoenix Fielding Details inserted ...")
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func saveMatchDetails(scorecard string, db *sql.DB) int {
	f, err := os.Open(scorecard)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		linetext = scanner.Text()
		totalLines = totalLines + 1

		// get Match details

		if totalLines == 1 {

			var re = regexp.MustCompile(`\b(Summer|Winter|Spring|Fall)(?:\W+\w+){1,6}?\W+(Div)[[:blank:]][A-Z]\b`)
			re = regexp.MustCompile(`(\d{1,4}([.\-/])\d{1,2}([.\-/])\d{1,4})`)

			if len(re.FindStringIndex(linetext)) > 0 {
				matchDate = re.FindString(linetext)
			}

			if strings.Contains(linetext, "-") {
				series = strings.Trim(strings.Split(linetext, "-")[0], " ")
			} else {
				series = "Spring 2022"
			}

			division = strings.Trim(strings.Split(linetext, ":")[0], " ")

			re = regexp.MustCompile("League|Finals|3rd Position")
			if len(re.FindStringIndex(linetext)) > 0 {
				stage = re.FindString(linetext)
			}

			result = strings.Replace(linetext, strings.Split(linetext, "League")[0], "", 1)
			result = strings.Replace(result, "("+matchDate+")", "", 1)
			result = strings.Trim(strings.Replace(result, "League", "", 1), " ")
		}

		// Team1 & Team2
		if totalLines == 2 {
			teams = strings.Split(linetext, "Vs")
			team1 = strings.Trim(teams[0], " ")
			team2 = strings.Trim(teams[1], " ")
		}

		if totalLines > 2 {
			break
		}

		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}

	InsertMatchDetails(db, series, stage, division, matchDate, team1, team2, result)
	return getMatchId(db)
}

func getMatchId(db *sql.DB) int {

	row, err := db.Query("SELECT MAX(matchid) from match")
	if err != nil {
		log.Fatal(err)
	}
	defer row.Close()
	for row.Next() {
		row.Scan(&matchid)
	}
	return matchid
}

func Dbconnect() *sql.DB {
	log.Println("Creating SQLite3 connection to phoenixPoints db...")
	phoenixdb, _ := sql.Open("sqlite3", "./phoenixPoints.db")
	log.Println("Connection created to phoenixPoints db..")
	return phoenixdb
}

func CreateTables(db *sql.DB) {
	createMatchTableSQL := `CREATE TABLE IF NOT EXISTS match (
		"matchid" integer NOT NULL PRIMARY KEY AUTOINCREMENT,		
		"series" TEXT,
		"stage" TEXT,
		"division" TEXT	,
		"matchDate" TEXT,
		"Team1" TEXT,
		"Team2" TEXT,
		"Result" TEXT	
	  );`

	createPhoenixBowlers := `CREATE TABLE IF NOT EXISTS bowlers (
		"matchid" INTEGER,
		"bowlerName" TEXT,
		"overs" TEXT,
		"Maidens" INTEGER DEFAULT 0,
		"RunsGiven" INTEGER DEFAULT 0,
		"wickets" INTEGER DEFAULT 0,
		"Wides" INTEGER DEFAULT 0,
		"NoBalls" INTEGER DEFAULT 0
	  );`

	createPhoenixBatsmen := `CREATE TABLE IF NOT EXISTS batsmen (
		"matchid" INTEGER,
		"battername" TEXT,
		"runs" INTEGER DEFAULT 0,
		"balls" INTEGER DEFAULT 0,
		"fours" INTEGER DEFAULT 0,
		"sixers" INTEGER DEFAULT 0,
		"Notout" INTEGER DEFAULT 0
	  );`

	createPhoenixFielding := `CREATE TABLE IF NOT EXISTS fielders (
		"matchid" INTEGER,
		"Batsman" TEXT,
		"wicketType" TEXT,
		"fieldername" TEXT,
		"bowlername" TEXT,
		"bowled" INTEGER DEFAULT 0,
		"catches" INTEGER DEFAULT 0,
		"runouts" INTEGER DEFAULT 0
	  );`

	/* createPointsTableSQL := `CREATE TABLE IF NOT EXISTS points (
		"matchid" integer ,
		"player" TEXT,
		"RunsScored" INTEGER DEFAULT 0,
		"Boundries" INTEGER DEFAULT 0,
		"NotOut"	INTEGER DEFAULT 0,
		"Duck"		INTEGER DEFAULT 0,
		"Wicket"	INTEGER DEFAULT 0,
		"Bowled"	INTEGER DEFAULT 0,
		"Maiden"	INTEGER DEFAULT 0,
		"BNetRR"	INTEGER DEFAULT 0,
		"Extras"	INTEGER DEFAULT 0,
		"Catch"		INTEGER DEFAULT 0,
		"CaughtAndBowled"	INTEGER DEFAULT 0,
		"Runout"	INTEGER DEFAULT 0,
		"DropCatch" INTEGER DEFAULT 0,
		"MatchResult"	INTEGER DEFAULT 0,
		"Total"		INTEGER	DEFAULT 0
	  );` */

	execQuery(db, createMatchTableSQL, "Creating Match Table")
	execQuery(db, createPhoenixBowlers, "Creating bowlers Table")
	execQuery(db, createPhoenixBatsmen, "Creating Batter Table")
	execQuery(db, createPhoenixFielding, "Creating Fielders Table")
	//execQuery(db, createPointsTableSQL, "Creating Points Table")

}

func InsertMatchDetails(db *sql.DB, series string, stage string, division string, matchDate string, team1 string, team2 string, result string) {
	log.Println("Inserting Match details...")
	insertStudentSQL := `INSERT INTO match (series,stage,division,matchDate,Team1,Team2,Result) VALUES (?, ?, ?,?, ?, ?, ? )`
	statement, err := db.Prepare(insertStudentSQL) // Prepare statement.
	// This is good to avoid SQL injections
	if err != nil {
		log.Fatalln(err.Error())
	}
	_, err = statement.Exec(series, stage, division, matchDate, team1, team2, result)
	if err != nil {
		log.Fatalln(err.Error())
	}
	log.Println("Match Details inserted ...")
}

func execQuery(db *sql.DB, query string, querycomment string) {
	statement, err := db.Prepare(query) // Prepare statement.
	// This is good to avoid SQL injections
	if err != nil {
		log.Fatalln(err.Error())
	}
	_, err = statement.Exec()
	if err != nil {
		log.Fatalln(err.Error())
	}
	log.Println(querycomment)
}

func getOpponent(db *sql.DB, matchid int) string {

	var t1, t2, oppo string

	row, err := db.Query("select Team1 , Team2 from match where matchid =" + strconv.Itoa(matchid))
	if err != nil {
		log.Fatal(err)
	}
	defer row.Close()
	for row.Next() {
		row.Scan(&t1, &t2)
		if t1 == "Phoenix" {
			oppo = t2
		} else {
			oppo = t1
		}
	}
	return oppo
}

func getFullPlayerName(db *sql.DB, matchid int, partialName string) string {

	var searchString, fullPlayerName, searchSQL string
	searchString = partialName + "%"
	searchSQL = "select battername  from batsmen b where matchid = ? AND battername like ?"

	stmt, err := db.Prepare(searchSQL)
	if err != nil {
		log.Fatal(err)
	}

	err = stmt.QueryRow(matchid, searchString).Scan(&fullPlayerName)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Println("No Rows Returned")
		}
	}
	return fullPlayerName
}

func getPlayersInthisMatch(db *sql.DB, matchid int) [11]string {

	var Players [11]string
	i := 0
	pSQL := "select battername  from batsmen b where matchid = ? LIMIT 11"

	stmt, err := db.Prepare(pSQL)
	if err != nil {
		log.Fatal(err)
	}

	//err = stmt.QueryRow(matchid).Scan(&Players)
	row, err := stmt.Query(matchid)
	if err != nil {
		log.Fatal(err)
	}
	for row.Next() {
		row.Scan(&Players[i])
		i = i + 1
	}
	return Players
}

func getBowlersInthisMatch(db *sql.DB, matchid int) [11]string {

	var Bowlers [11]string
	i := 0
	pSQL := "select bowlerName  from bowlers b where matchid = ?"

	stmt, err := db.Prepare(pSQL)
	if err != nil {
		log.Fatal(err)
	}

	//err = stmt.QueryRow(matchid).Scan(&Players)
	row, err := stmt.Query(matchid)
	if err != nil {
		log.Fatal(err)
	}
	for row.Next() {
		row.Scan(&Bowlers[i])
		i = i + 1
	}
	return Bowlers
}

func checkPlayerThere(db *sql.DB, matchid int, playerName string) bool {

	var pname string
	pSQL := "select battername  from batsmen  where matchid =" + strconv.Itoa(matchid) + " AND TRIM(battername) = TRIM(\"" + playerName + "\") LIMIT 1;"
	stmt, err := db.Prepare(pSQL)
	if err != nil {
		log.Fatal(err)
	}
	err = stmt.QueryRow().Scan(&pname)
	if err != nil {
		if err == sql.ErrNoRows {
			return false
		}
	}
	return true
}

func replacePlayerAllTables(db *sql.DB, matchid int, playerName [11]string, newplayerName [11]string) {

	updateBatsmenSQL := `update batsmen SET battername=TRIM(?) where TRIM(battername)=TRIM(?) AND matchid = ?`
	updateBowlerSQL := `update bowlers SET bowlerName=TRIM(?) where TRIM(bowlerName)=TRIM(?) AND matchid = ?`
	updateFieldersSQL1 := `update fielders SET fieldername=TRIM(?) where TRIM(fieldername)=TRIM(?) AND matchid = ?`
	updateFieldersSQL2 := `update fielders SET bowlername=TRIM(?) where TRIM(bowlername)=TRIM(?) AND matchid = ?`
	updatePointsSQL := `update TotalMatchPoints SET Player=TRIM(?) where TRIM(Player)=TRIM(?) AND matchid = ?`

	for i := 0; i < len(playerName); i++ {
		execPlayerUpdateQuery(db, updateBatsmenSQL, "Batsmen Table Updated.....", matchid, playerName[i], newplayerName[i])
		execPlayerUpdateQuery(db, updateBowlerSQL, "Bowlers Table Updated.....", matchid, playerName[i], newplayerName[i])
		execPlayerUpdateQuery(db, updateFieldersSQL1, "Fielder Table Updated for Fielder Names.....", matchid, playerName[i], newplayerName[i])
		execPlayerUpdateQuery(db, updateFieldersSQL2, "Fielder Table Updated for Bowler Names.....", matchid, playerName[i], newplayerName[i])
		execPlayerUpdateQuery(db, updatePointsSQL, "Points Table updated for Player Name .....", matchid, playerName[i], newplayerName[i])
	}

	log.Println("All Name updates Done ...")
}

func execPlayerUpdateQuery(db *sql.DB, query string, querycomment string, matchid int, playerName string, newPlayerName string) {

	statement, err := db.Prepare(query) // Prepare statement.
	// This is good to avoid SQL injections
	if err != nil {
		log.Fatalln(err.Error())
	}

	_, err = statement.Exec(newPlayerName, playerName, matchid)

	if err != nil {
		log.Fatalln(err.Error())
	}
}

func exec1RunOverUpdate(db *sql.DB, matchid int, Bowlers [11]string, OneRunOvers [11]int) {

	updateQuery := `UPDATE TotalMatchPoints SET OneRunOvers= ?*5 WHERE matchid=? AND TRIM(Player) = TRIM(?)`
	statement, err := db.Prepare(updateQuery) // Prepare statement.
	// This is good to avoid SQL injections
	if err != nil {
		log.Fatalln(err.Error())
	}

	for i := 0; i < len(Bowlers); i++ {
		if Bowlers[i] != "" {
			_, err = statement.Exec(OneRunOvers[i], matchid, Bowlers[i])
		}
	}

	if err != nil {
		log.Fatalln(err.Error())
	}
	log.Println("1 Run Overs Updated... ")
}

func exec1DropCatches(db *sql.DB, matchid int, Players [11]string, DropCatches [11]int) {

	updateQuery := `UPDATE TotalMatchPoints SET DropCatches = ?*-3 WHERE matchid=? AND TRIM(Player) = TRIM(?)`
	statement, err := db.Prepare(updateQuery) // Prepare statement.
	// This is good to avoid SQL injections
	if err != nil {
		log.Fatalln(err.Error())
	}

	for i := 0; i < len(Players); i++ {
		_, err = statement.Exec(DropCatches[i], matchid, Players[i])
	}

	if err != nil {
		log.Fatalln(err.Error())
	}
	log.Println("Drop Catches Updated... ")
}

func calculatePoints(db *sql.DB) {

	dropPointsTableSQL := `DROP TABLE IF EXISTS TotalMatchPoints`
	createPointsTableSQL := `CREATE TABLE TotalMatchPoints AS 

	select
		* ,
		0 as "OneRunOvers",
		0 as "DropCatches",
		T.RunsScored + T.Boundries + T.NotOut + T.Duck + T.wicket + T.Maidens + T.NRR + T.Extras + T.Bowled + T.catch + T.runouts + T.MatchWon as "Total Points"
	from
		(
		SELECT
			b.matchid,
			b.battername "Player",
			b.runs * 2 as "RunsScored" ,
			b.fours * 5 as "Boundries" ,
			b.Notout * 2 as "NotOut" ,
			CASE
				WHEN (b.runs > 0
				AND b.balls>0) THEN 0
				WHEN b .balls = 0 THEN 0
				ELSE -3
			END as "Duck",
			CASE
				WHEN w.wickets > 0 THEN w.wickets * 10
				ELSE 0
			END as "wicket",
			CASE
				WHEN w.Maidens > 0 THEN w.Maidens * 5
				ELSE 0
			END as "Maidens",
			CASE
				WHEN (w.RunsGiven / w.overs) <= 5 THEN 5
				WHEN (w.RunsGiven / w.overs)>= 7 THEN -3
				ELSE 0
			END as "NRR",
			CASE
				WHEN w.overs > 0 THEN
				(CASE
					WHEN (w.Wides + w.NoBalls) >= 1 THEN (w.Wides + w.NoBalls)*-2
					ELSE 3
				END)
				ELSE 0
			END as "Extras",
			CASE
				WHEN p.Bowled > 0 THEN p.Bowled
				ELSE 0
			END as "Bowled",
			CASE
				WHEN q.catch > 0 THEN q.catch
				ELSE 0
			END as "catch",
			CASE
				WHEN q.runouts > 0 THEN q.runouts
				ELSE 0
			END as "runouts",
			CASE
				WHEN m."Result" Like "Phoenix Won%" THEN 10
				ELSE 0
			END as "MatchWon",
			m.matchDate "MatchDate",
			CASE
				WHEN m.Team1 = "Phoenix" THEN m.Team2
				ELSE m.Team1
			END as "Opponent"
		FROM
			batsmen b
		LEFT JOIN bowlers w ON
			b.matchid = w.matchid
			AND b.battername = w.bowlerName
		LEFT JOIN 
	(
			SELECT
				b1.battername as "battername",
				sum(f1.bowled * 2) as "Bowled"
			FROM
				batsmen b1
			JOIN fielders f1 ON
				b1.matchid = f1.matchid
				AND b1.battername = f1.bowlername
			group by
				b1.battername) p ON
			b.battername = p.battername
		LEFT JOIN
	(
			SELECT
				b2.battername "battername",
				sum(f2.catches * 8) as "catch",
				CASE WHEN f2.wicketType="RunOut-DirectHit" THEN sum(f2.runouts * 4) ELSE sum(f2.runouts * 3)  END as "runouts"
			FROM
				batsmen b2
			JOIN fielders f2 ON
				b2.matchid = f2.matchid
				AND b2.battername = f2.fieldername
			group by
				b2.battername ) q ON
			b.battername = q.battername
		JOIN "match" m ON
			b.matchid = m.matchid ) T
	`
	execQuery(db, dropPointsTableSQL, "Dropping Points Table")
	execQuery(db, createPointsTableSQL, "Creating Points Table with this Match details.....")
}

func calculatePointsFinal(db *sql.DB) {
	updatePointsTableSQL := `UPDATE TotalMatchPoints SET "Total Points"=RunsScored + Boundries + NotOut + Duck + wicket + Maidens + NRR + Extras + Bowled + catch + runouts + MatchWon + OneRunOvers + DropCatches`
	execQuery(db, updatePointsTableSQL, "Final Point Update Done .... ")
}

func extractRanges(scorecard string) ([]string, []string, []string) {

	phoBowStart, phoBowEnd := calculateRanges(scorecard, "Phoenix Bowling", "Total,")
	phoBatStart, phoBatEnd := calculateRanges(scorecard, "Phoenix Batting", "Byes:")
	phoFieldingStart, phoFieldingEnd := calculateRanges(scorecard, opponent+" Batting", "Byes:")
	return extractRange(scorecard, phoBowStart, phoBowEnd, "Phoenix Bowling", "Total,"), extractRange(scorecard, phoBatStart, phoBatEnd, "Phoenix Batting", "Byes:"), extractRange(scorecard, phoFieldingStart, phoFieldingEnd, opponent+" Batting", "Byes:")
}

func calculateRanges(scorecard string, startPattern string, endPattern string) (int, int) {
	f0, err := os.Open(scorecard)
	defer f0.Close()
	if err != nil {
		log.Fatal(err)
	}
	var linetext0 string
	scanner0 := bufio.NewScanner(f0)
	currentline0 := 0
	Indicator := false
	startPosition := 0
	endPosition := 0

	for scanner0.Scan() {
		linetext0 = scanner0.Text()
		currentline0 = currentline0 + 1

		if strings.Contains(linetext0, startPattern) {
			startPosition = currentline0
			Indicator = true
			continue
		}
		if strings.Contains(linetext0, endPattern) && currentline0 <= startPosition || (!Indicator) {
			continue
		}
		if strings.Contains(linetext0, endPattern) && currentline0 > startPosition && Indicator {
			endPosition = currentline0
			break
		}
	}
	return startPosition, endPosition
}

func extractRange(scorecard string, startposition int, endposition int, startPattern string, endPattern string) []string {
	var linetext0 string
	f0, err := os.Open(scorecard)
	defer f0.Close()
	if err != nil {
		log.Fatal(err)
	}
	scanner0 := bufio.NewScanner(f0)
	currentline0 := 0
	rangevalues := make([]string, 0)

	for scanner0.Scan() {
		linetext0 = scanner0.Text()
		currentline0 = currentline0 + 1

		if currentline0 < startposition && currentline0 >= 0 {
			continue
		} else if currentline0 <= endposition && currentline0 >= startposition {
			if len(strings.TrimSpace(linetext0)) > 0 {
				if !(strings.Contains(linetext0, startPattern) || strings.Contains(linetext0, endPattern)) {
					rangevalues = append(rangevalues, strings.TrimSpace(linetext0))
				}
			}
		} else if currentline0 > endposition {
			break
		}
	}
	return rangevalues
}

func dumpPointsTableAsCSV() {
	err, out, errout := Shellout(`sqlite3 -header -csv ./phoenixPoints.db  "select * from TotalMatchPoints where matchid=` + strconv.Itoa(matchid) + `;" > points_` + strconv.Itoa(matchid) + `.csv`)
	if err != nil {
		log.Printf("error: %v\n", err)
		fmt.Println(errout)
	}

	// Print the output
	log.Println(string(out + " CSV file : PhoenixPoints.csv created "))
}

func Shellout(command string) (error, string, string) {
	const ShellToUse = "bash"
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(ShellToUse, "-c", command)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return err, stdout.String(), stderr.String()
}

func processPlayerSwap(db *sql.DB) {

	var players [11]string
	var replacedplayers [11]string
	reader := bufio.NewReader(os.Stdin)

	players = getPlayersInthisMatch(db, matchid)
	for i := 0; i < len(players); i++ {
		fmt.Print("Player: " + players[i] + " Replace With  ? [" + players[i] + "] ")

		text, _ := reader.ReadString('\n')
		text = strings.Replace(text, "\n", "", -1)
		if text == "" {
			replacedplayers[i] = players[i]
		} else {
			replacedplayers[i] = text
		}
	}

	replacePlayerAllTables(db, matchid, players, replacedplayers)

}

func process1RunOverUpdate(db *sql.DB) {
	var bowlers [11]string
	var OneRunOverInput [11]int
	reader := bufio.NewReader(os.Stdin)

	bowlers = getBowlersInthisMatch(db, matchid)
	for i := 0; i < len(bowlers); i++ {
		if bowlers[i] != "" {
			fmt.Print("Bowler: " + bowlers[i] + " 1 Run overs ? [0] ")
			text := "0"
			text, _ = reader.ReadString('\n')
			text = strings.Replace(text, "\n", "", -1)
			OneRunOverInput[i], _ = strconv.Atoi(string(text))
		}
	}
	exec1RunOverUpdate(db, matchid, bowlers, OneRunOverInput)
}

func processDropCatches(db *sql.DB) {
	var players [11]string
	var DropCatchesInput [11]int
	reader := bufio.NewReader(os.Stdin)

	players = getPlayersInthisMatch(db, matchid)
	for i := 0; i < len(players); i++ {
		fmt.Print("Player: " + players[i] + " Drop Catches ? [0] ")
		text := "0"
		text, _ = reader.ReadString('\n')
		text = strings.Replace(text, "\n", "", -1)
		DropCatchesInput[i], _ = strconv.Atoi(string(text))
	}
	exec1DropCatches(db, matchid, players, DropCatchesInput)
}

func replacePlayer(db *sql.DB) {

	fmt.Println()
	fmt.Println("------------------------------------------")
	fmt.Println("Corrections & Other Updates")
	fmt.Println("------------------------------------------")
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("Do you want to Replace a Player Name with other Name ? (Yes Or No) (Default:No) ")
		fmt.Print("==> ")
		text, _ := reader.ReadString('\n')
		text = strings.Replace(text, "\n", "", -1)

		if strings.ToLower(text) == "no" || strings.ToLower(text) == "n" || strings.ToLower(text) == "" {
			break
		} else if strings.ToLower(text) == "yes" || strings.ToLower(text) == "y" {
			processPlayerSwap(db)
		} else {
			fmt.Println("Please Type Yes/Y/yes/y or No/N/n/no ")
		}
	}

	for {
		fmt.Println("Do you want to Enter 1 Run Overs (Maiden overs been already taken cared) ? (Yes Or No) (Default:No) ")
		fmt.Print("==> ")
		text, _ := reader.ReadString('\n')
		text = strings.Replace(text, "\n", "", -1)

		if strings.ToLower(text) == "no" || strings.ToLower(text) == "n" || strings.ToLower(text) == "" {
			break
		} else if strings.ToLower(text) == "yes" || strings.ToLower(text) == "y" {
			process1RunOverUpdate(db)
		} else {
			fmt.Println("Please Type Yes/Y/yes/y or No/N/n/no ")
		}
	}

	for {
		fmt.Println("Do you want to Enter Drop Catches by Players ? (Yes Or No) (Default:No) ")
		fmt.Print("==> ")
		text, _ := reader.ReadString('\n')
		text = strings.Replace(text, "\n", "", -1)

		if strings.ToLower(text) == "no" || strings.ToLower(text) == "n" || strings.ToLower(text) == "" {
			break
		} else if strings.ToLower(text) == "yes" || strings.ToLower(text) == "y" {
			processDropCatches(db)
		} else {
			fmt.Println("Please Type Yes/Y/yes/y or No/N/n/no ")
		}
	}
	calculatePointsFinal(db)
}

func renderFinalTable(db *sql.DB) {

	//==========================================================================
	// Initialization
	//==========================================================================
	t := table.NewWriter()
	// you can also instantiate the object directly
	tTemp := table.Table{}
	tTemp.Render() // just to avoid the compile error of not using the object
	//==========================================================================

	rowHeader := table.Row{"S.No", "Player", "Total Points"}

	renderSQL := `select Player , "Total Points"  from TotalMatchPoints where matchid = ? order by "Total Points" DESC LIMIT 11`
	var PlayerName string
	var TotalPoints, i int
	i = 0

	stmt, err := db.Prepare(renderSQL)
	if err != nil {
		log.Fatal(err)
	}

	row, err := stmt.Query(matchid)
	if err != nil {
		log.Fatal(err)
	}
	for row.Next() {
		row.Scan(&PlayerName, &TotalPoints)
		t.AppendRow(table.Row{i + 1, PlayerName, TotalPoints})
		i = i + 1

	}
	t.AppendHeader(rowHeader)
	fmt.Println("------------------------------------------")
	fmt.Println("Total Points for this Match")
	fmt.Println("------------------------------------------")
	fmt.Println(t.Render())
}
