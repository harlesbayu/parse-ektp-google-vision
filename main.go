package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"regexp"
	"strings"
)

type Vertice struct {
	X int64
	Y int64
}

type BoundingPoly struct {
	Vertices []Vertice
}

type TextAnnotation struct {
	Locale       string
	Description  string
	BoundingPoly BoundingPoly
}

type Response struct {
	TextAnnotations []TextAnnotation
}

type OCRResponse struct {
	Responses []Response
}

func main() {

	var result OCRResponse
	// open and unmarshall json file
	jsonFile, _ := os.Open("data.json")
	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)
	json.Unmarshal(byteValue, &result)

	type EktpData struct {
		provinsi         string
		kota             string
		nik              string
		nama             string
		ttl              string
		jenisKelamin     string
		agama            string
		statusPerkawinan string
		pekerjaan        string
		kewarganegaraan  string
		berlakuHingga    string
		alamat           string
		kecamatan        string
		kelurahan        string
	}

	var ektp EktpData

	lines := strings.Split(strings.ReplaceAll(result.Responses[0].TextAnnotations[0].Description, "\r\n", "\n"), "\n")

	RegexNIK, _ := regexp.Compile(`[\d]{5,}`)
	RegexWordOfTwoOrMoreUpperCase, _ := regexp.Compile(`[A-Z]{2,}`)
	RegexDateChars, _ := regexp.Compile(`[0-9-]`)

	// 1. find provinsi and kota
	for i := 0; i < len(lines); i++ {
		uppercaseWords := RegexWordOfTwoOrMoreUpperCase.FindAllString(lines[i], -1)
		lineCleaned := strings.Join(strings.Split(lines[i], " "), "")

		if ektp.provinsi != "" && ektp.kota != "" {
			break
		}
		if RegexNIK.MatchString(lineCleaned) {
			break
		}
		if len(uppercaseWords) == 0 {
			continue
		}

		if ektp.provinsi == "" {
			ektp.provinsi = strings.Join(uppercaseWords, " ")
			continue
		}

		if ektp.kota == "" {
			ektp.kota = strings.Join(uppercaseWords, " ")
			continue
		}
	}

	// 2. find nik value
	for i := 0; i < len(lines); i++ {
		lineCleaned := strings.Join(strings.Split(lines[i], " "), "")
		nik := RegexNIK.FindAllString(lineCleaned, -1)
		if len(nik) > 0 && len(nik[0]) == 16 {
			ektp.nik = nik[0]
			break
		}
	}

	// 3. find nik vertices/coordinates needed to define content's bounding box
	// we use loop & chomping bcs there are cases of nik splitted to multiple words
	nikVx := make([]Vertice, 4)
	nikChomped := ektp.nik
	for i := 1; i < len(result.Responses[0].TextAnnotations); i++ {
		text := result.Responses[0].TextAnnotations[i]
		word := text.Description
		wordVx := text.BoundingPoly.Vertices

		if nikChomped == "" {
			break
		}
		if strings.Index(nikChomped, word) != -1 {
			nikChomped = strings.Replace(nikChomped, word, "", -1)

			// assign nik vertices: aki, aka, kaba, kiba. w/ inverted y axis (topneg, botpos)
			nikVx[0].X = int64(math.Min(float64(max(nikVx[0].X, wordVx[0].X)), float64(wordVx[0].X)))
			nikVx[0].Y = int64(math.Min(float64(max(nikVx[0].Y, wordVx[0].Y)), float64(wordVx[0].Y)))

			nikVx[1].X = int64(math.Max(float64(max(nikVx[1].X, wordVx[1].X)), float64(wordVx[1].X)))
			nikVx[1].Y = int64(math.Min(float64(max(nikVx[1].Y, wordVx[1].Y)), float64(wordVx[1].Y)))

			nikVx[2].X = int64(math.Max(float64(max(nikVx[2].X, wordVx[2].X)), float64(wordVx[2].X)))
			nikVx[2].Y = int64(math.Max(float64(max(nikVx[2].Y, wordVx[2].Y)), float64(wordVx[2].Y)))

			nikVx[3].X = int64(math.Min(float64(max(nikVx[3].X, wordVx[3].X)), float64(wordVx[3].X)))
			nikVx[3].Y = int64(math.Max(float64(max(nikVx[3].Y, wordVx[3].Y)), float64(wordVx[3].Y)))
		}
	}

	// 4. build ektp lines from words within bounding box
	type ContentBounds struct {
		topY   float64
		leftX  float64
		rightX float64
	}

	contentBounds := ContentBounds{
		topY:   math.Max(float64(nikVx[2].Y), float64(nikVx[3].Y)), // below nik
		leftX:  math.Min(float64(nikVx[0].X), float64(nikVx[3].X)), // within nik leftmost x and rightmost x
		rightX: math.Max(float64(nikVx[1].X), float64(nikVx[2].X)), // within nik leftmost x and rightmost x
	}

	contentBoundsHeight := 0.9 * math.Abs(contentBounds.leftX-contentBounds.rightX)

	yHeightTolerance := 0.5 * math.Abs(float64(nikVx[1].Y)-float64(nikVx[2].Y))                  // nik height
	xNikLeftPadTolerance := 0.125 * math.Abs(contentBounds.leftX-contentBounds.rightX)           // 1/8 of nik width (to handle light skewed scan condition)
	xDistTolerance := 0.1 * math.Abs(float64(contentBounds.leftX)-float64(contentBounds.rightX)) // nik width

	var builtLines []string
	var lastWordVx []Vertice
	var firstWordOfLineVx []Vertice
	for i := 1; i < len(result.Responses[0].TextAnnotations); i++ {
		text := result.Responses[0].TextAnnotations[i]
		word := text.Description
		wordVx := text.BoundingPoly.Vertices

		// clean invalid characters
		word = strings.TrimSpace(word)
		if strings.HasPrefix(word, ":") {
			word = word[1:len(word)]
		}

		if word == "" {
			continue
		}

		// select those only inside bounded box
		if float64(wordVx[0].X) > contentBounds.leftX-xNikLeftPadTolerance &&
			float64(wordVx[0].X) < contentBounds.rightX &&
			float64(wordVx[3].Y) > contentBounds.topY &&
			float64(wordVx[3].Y) < contentBounds.topY+contentBoundsHeight {

			if len(builtLines) == 0 {
				builtLines = append(builtLines, word)
				lastWordVx = wordVx
				firstWordOfLineVx = wordVx
				continue
			}

			// check if its in a new line
			dYLine := wordVx[0].Y - firstWordOfLineVx[0].Y
			if float64(dYLine) > yHeightTolerance {
				// find out how many lines to add from previous line
				lineCountsToAdd := math.Floor(float64(dYLine) / yHeightTolerance)
				for i := 0; i < int(lineCountsToAdd); i++ {
					builtLines = append(builtLines, "")
				}
				firstWordOfLineVx = wordVx
			}

			// check if its x spaced too far (case golongan darah)
			dX := wordVx[0].X - lastWordVx[1].X
			if float64(dX) < xDistTolerance {
				builtLines[len(builtLines)-1] += fmt.Sprintf(" %v", word)
				builtLines[len(builtLines)-1] = strings.TrimSpace(builtLines[len(builtLines)-1])
				lastWordVx = wordVx
			}
		}
	}

	// 5. assign other fields
	ektp.nama = builtLines[0]
	ektp.ttl = builtLines[1]
	ektp.jenisKelamin = builtLines[2]
	ektp.agama = builtLines[len(builtLines)-5]
	ektp.statusPerkawinan = builtLines[len(builtLines)-4]
	ektp.pekerjaan = builtLines[len(builtLines)-3]
	ektp.kewarganegaraan = builtLines[len(builtLines)-2]
	ektp.berlakuHingga = builtLines[len(builtLines)-1]

	// assign leftover in-betweens as alamat
	var alamat []string
	for i := 3; i < len(builtLines)-7; i++ {
		alamat = append(alamat, strings.TrimSpace(builtLines[i]))
	}
	ektp.alamat = strings.Join(alamat, " ")

	type Norm struct {
		nama             string
		pekerjaan        string
		agama            string
		kewarganegaraan  string
		provinsi         string
		kota             string
		kecamatan        string
		kelurahan        string
		jenisKelamin     string
		berlakuHingga    string
		statusPerkawinan string
		tll              struct {
			city string
			date string
		}
	}

	var norm Norm

	norm.nama = filterToCapitalWords(ektp.nama)
	norm.pekerjaan = filterToCapitalWords(ektp.pekerjaan)
	norm.agama = filterToCapitalWords(ektp.agama)
	norm.kewarganegaraan = filterToCapitalWords(ektp.kewarganegaraan)
	norm.provinsi = filterToCapitalWords(ektp.provinsi)
	norm.kota = filterToCapitalWords(ektp.kota)
	norm.kecamatan = filterToCapitalWords(ektp.kecamatan)
	norm.kelurahan = filterToCapitalWords(ektp.kelurahan)

	// + jenis kelamin
	if levdist(ektp.jenisKelamin, "PEREMPUAN") < 4 {
		norm.jenisKelamin = "PEREMPUAN"
	}
	if levdist(ektp.jenisKelamin, "LAKI-LAKI") < 4 {
		norm.jenisKelamin = "LAKI-LAKI"
	}

	// + ttl
	tempatLahir := RegexWordOfTwoOrMoreUpperCase.FindAllString(ektp.ttl, -1)
	if len(tempatLahir) != 0 {
		norm.tll.city = strings.Join(tempatLahir, " ")
	}
	tanggalLahir := RegexDateChars.FindAllString(ektp.ttl, -1)
	if len(tanggalLahir) != 0 {
		norm.tll.date = strings.Join(tanggalLahir, "")
	}

	// + berlakuHingga
	if levdist(ektp.berlakuHingga, "SEUMUR HIDUP") < 6 {
		norm.berlakuHingga = "SEUMUR HIDUP"
	}

	if norm.berlakuHingga == "" {
		berlakuHingga := RegexDateChars.FindAllString(ektp.berlakuHingga, -1)
		if len(berlakuHingga) != 0 {
			norm.berlakuHingga = strings.Join(berlakuHingga, "")
		}
	}

	// + statusPerkawinan
	arrMarital := [3]string{"Kawin", "Belum Kawin", "Cerai"}
	var maxValue float64
	for i := 0; i < len(arrMarital); i++ {
		currName := strings.ToLower(arrMarital[i])
		currValue := textCosineSimilarity(strings.ToLower(ektp.statusPerkawinan), currName)
		if currValue > maxValue {
			maxValue = currValue
			norm.statusPerkawinan = arrMarital[i]
		}
	}

	fmt.Println(ektp)
	fmt.Println(norm)
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func filterToCapitalWords(source string) string {
	RegexWordOfTwoOrMoreUpperCase, _ := regexp.Compile(`[A-Z]{2,}`)
	uppercaseWords := RegexWordOfTwoOrMoreUpperCase.FindAllString(source, -1)
	if len(uppercaseWords) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(uppercaseWords, " "))
}

func levdist(stra string, strb string) int {
	a := stra
	b := strb
	var i int
	var j int

	m := make([][]int, len(b)+1)
	for i := range m {
		m[i] = make([]int, len(a)+1)
		for j := range m[i] {
			m[i][j] = 0
		}
	}

	for i = 0; i <= len(b); i++ {
		m[i][0] = i
	}

	for j = 0; j <= len(a); j++ {
		m[0][j] = j
	}

	for i = 1; i <= len(b); i++ {
		for j = 1; j <= len(a); j++ {
			if string(b[i-1]) == string(a[j-1]) {
				m[i][j] = m[i-1][j-1]
			} else {
				m[i][j] = int(math.Min(float64(m[i-1][j-1]+1), math.Min(float64(m[i][j-1]+1), float64(m[i-1][j]+1))))
			}
		}
	}
	return m[len(b)][len(a)]
}

//consine similarity
func termFreqMap(str string) map[string]int {
	words := strings.Split(str, " ")
	var termFreq = make(map[string]int)

	for _, s := range words {
		termFreq[s] = 1
	}

	return termFreq
}

func addKeysToDict(datas map[string]int, dict map[string]bool) {
	for key, _ := range datas {
		dict[key] = true
	}
}

func termFreqMapToVector(datas map[string]int, dict map[string]bool) []int {
	termFreqVector := []int{}
	for key, _ := range dict {
		termFreqVector = append(termFreqVector, datas[key])
	}

	return termFreqVector
}

func vecDotProduct(vecA []int, vecB []int) float64 {
	product := 0
	for i := 0; i < len(vecA); i++ {
		product += vecA[i] * vecB[i]
	}
	return float64(product)
}

func vecMagnitude(vec []int) float64 {
	sum := 0
	for i := 0; i < len(vec); i++ {
		sum += vec[i] * vec[i]
	}
	return math.Sqrt(float64(sum))
}

func cosineSimilarity(vecA []int, vecB []int) float64 {
	return vecDotProduct(vecA, vecB) / (vecMagnitude(vecA) * vecMagnitude(vecB))
}

func textCosineSimilarity(strA string, strB string) float64 {
	termFreqA := termFreqMap(strA)
	termFreqB := termFreqMap(strB)

	var dict = map[string]bool{}

	addKeysToDict(termFreqA, dict)
	addKeysToDict(termFreqB, dict)

	termFreqVecA := termFreqMapToVector(termFreqA, dict)
	termFreqVecB := termFreqMapToVector(termFreqB, dict)
	return cosineSimilarity(termFreqVecA, termFreqVecB)
}
