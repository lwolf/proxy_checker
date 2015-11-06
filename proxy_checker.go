package main

import (
	"flag"
	"fmt"
	"github.com/abh/geoip"
	"github.com/parnurzeal/gorequest"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const TESTURL = "https://api.github.com"

type Proxy struct {
	Scheme  string `bson:"protocol"`
	Host    string `bson:"host"`
	Port    string `bson:"port"`
	Country string `bson:"country"`
	Status  bool   `bson:"status"`
}

func (p *Proxy) getURI() string {
	return fmt.Sprintf("%s://%s:%s", p.Scheme, p.Host, p.Port)
}

func (p *Proxy) toString() string {
	return fmt.Sprintf("<%s://%s:%s [%s]>", p.Scheme, p.Host, p.Port, p.Country)
}

func checkProxy(p Proxy) bool {
	var isAlive bool
	request := gorequest.New().Proxy(p.getURI())
	resp, _, _ := request.Get(TESTURL).End()
	if resp.Status == "200 OK" {
		isAlive = true
	} else {
		isAlive = false
	}
	fmt.Sprintf("%s is %s", p.getURI(), isAlive)
	return isAlive
}

func updateProxy(p Proxy, status bool, mongo mgo.Collection) {
	err := mongo.Update(bson.M{"host": p.Host, "port": p.Port}, bson.M{"$set": bson.M{"status": status, "country": p.Country}})
	if err != nil {
		log.Fatal(err)
	}
}

//Load proxies from fineproxy account
func downloadProxy(mongo mgo.Collection, login string, password string, g geoip.GeoIP) {
	request_url := "http://account.fineproxy.org/api/getproxy/"
	parsed_request_url, _ := url.Parse(request_url)
	url_params := url.Values{
		"format":   {"txt"},
		"type":     {"httpip"},
		"login":    {login},
		"password": {password},
	}
	parsed_request_url.RawQuery = url_params.Encode()
	request_url = parsed_request_url.String()

	resp, err := http.Get(request_url)
	defer resp.Body.Close()
	if err != nil {
		panic(err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	var proxies []Proxy
	proxies_list := strings.Split(string(body), "\r\n")
	for _, value := range proxies_list {
		if !strings.Contains(value, ":") {
			continue
		}
		host, port, _ := net.SplitHostPort(value)
		country, _ := g.GetCountry(host)
		proxy := Proxy{"http", host, port, country, false}
		proxies = append(proxies, proxy)
	}
	// var c chan map[Proxy string] = make(chan map[Proxy string])
	for _, proxy := range proxies {
		go updateProxy(proxy, checkProxy(proxy), mongo)
	}
}

func checkProxies(mongo mgo.Collection) {
	var result Proxy
	iter := mongo.Find(nil).Iter()
	for iter.Next(&result) {
		updateProxy(result, checkProxy(result), mongo)
	}
	if err := iter.Close(); err != nil {
		log.Fatal(err)
	}

}

func main() {
	var runMode = flag.String("mode", "check", "Specify mode to run. `download` new or `check` existant proxies")
	var login = flag.String("login", "", "Login to fineproxy for download mode")
	var password = flag.String("password", "", "Password to fineproxy for download mode")
	var host = flag.String("host", "localhost", "Mongodb host")
	var port = flag.Int("port", 27017, "Mongodb port")
	var database = flag.String("database", "proxy", "Mongodb database to read/write proxies")
	var collection = flag.String("collection", "proxies", "Mongodb collection to read/write proxies")

	flag.Parse()
	geoIP, err := geoip.Open("/usr/share/GeoIP/GeoIP.dat")
	if err != nil {
		fmt.Printf("Could not open GeoIP database: %s\n", err)
	}
	if *runMode == "download" {
		if *login == "" || *password == "" {
			println("ERROR: You must provide login and password for fineproxy to use this mode")
			return
		}
	}
	mongo, err := mgo.Dial(fmt.Sprintf("mongodb://%s:%d", *host, *port))
	if err != nil {
		panic(err)
	}
	defer mongo.Close()
	connection := mongo.DB(*database).C(*collection)
	// ensure index in collection for unique `host + port`
	switch *runMode {
	case "download":
		println("Going to download proxies from remote...")
		downloadProxy(*connection, *login, *password, *geoIP)
	case "check":
		checkProxies(*connection)
		println("Going to recheck all available proxies...")
	}
}
