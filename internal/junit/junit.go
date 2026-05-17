package junit

import (
	"encoding/xml"
	"fmt"
)

type Suite struct {
	Name  string
	Cases []Case
}

type Case struct {
	Name           string
	ClassName      string
	Status         string
	TimeSeconds    float64
	FailureMessage string
	Output         string
}

type xmlSuite struct {
	XMLName  xml.Name  `xml:"testsuite"`
	Name     string    `xml:"name,attr"`
	Tests    int       `xml:"tests,attr"`
	Failures int       `xml:"failures,attr"`
	Skipped  int       `xml:"skipped,attr"`
	Time     string    `xml:"time,attr"`
	Cases    []xmlCase `xml:"testcase"`
}

type xmlCase struct {
	Name      string      `xml:"name,attr"`
	ClassName string      `xml:"classname,attr,omitempty"`
	Time      string      `xml:"time,attr"`
	Failure   *xmlFailure `xml:"failure,omitempty"`
	Skipped   *xmlSkipped `xml:"skipped,omitempty"`
	Output    string      `xml:"system-out,omitempty"`
}

type xmlFailure struct {
	Message string `xml:"message,attr,omitempty"`
	Body    string `xml:",chardata"`
}

type xmlSkipped struct{}

func Render(suite Suite) ([]byte, error) {
	payload := xmlSuite{Name: suite.Name, Tests: len(suite.Cases)}
	totalSeconds := 0.0
	for _, item := range suite.Cases {
		totalSeconds += item.TimeSeconds
		row := xmlCase{
			Name:      item.Name,
			ClassName: item.ClassName,
			Time:      formatSeconds(item.TimeSeconds),
			Output:    item.Output,
		}
		switch item.Status {
		case "failed", "fail", "error":
			payload.Failures++
			row.Failure = &xmlFailure{Message: item.FailureMessage, Body: item.Output}
		case "skipped", "skip":
			payload.Skipped++
			row.Skipped = &xmlSkipped{}
		}
		payload.Cases = append(payload.Cases, row)
	}
	payload.Time = formatSeconds(totalSeconds)
	raw, err := xml.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(raw, '\n')...), nil
}

func formatSeconds(value float64) string {
	return fmt.Sprintf("%.3f", value)
}
