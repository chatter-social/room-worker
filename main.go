package main

// Note - need to add case to start audio egress for scheduled rooms.
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/chatter-social/room-worker/db"
	"github.com/fatih/color"
	"github.com/joho/godotenv"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go"
)

type Room struct {
	Name            string
	NumParticipants uint32
	MediaType       string
}

type Meta struct {
	Count int `json:"count"`
}

type Response struct {
	Meta Meta `json:"meta"`
}

func fetchLiveRooms(client *db.PrismaClient, ctx context.Context) {
	// Get all live rooms
	hostURL := os.Getenv("LIVEKIT_HOST")
	apiKey := os.Getenv("LIVEKIT_API_KEY")
	apiSecret := os.Getenv("LIVEKIT_API_KEY_SECRET")
	emqxHost := os.Getenv("EMQX_HOST")
	emqxPort := os.Getenv("EMQX_PORT")

	roomClient := lksdk.NewRoomServiceClient(hostURL, apiKey, apiSecret)

	// list rooms
	rooms := make([]*Room, 0)
	res, _ := roomClient.ListRooms(context.Background(), &livekit.ListRoomsRequest{})
	for _, room := range res.Rooms {
		rooms = append(rooms, &Room{
			Name:            room.Name,
			NumParticipants: (room.GetNumParticipants()),
			MediaType:       "AudioOnly",
		})
	}
	sort.Slice(rooms, func(i, j int) bool {
		return rooms[i].NumParticipants > rooms[j].NumParticipants
	})
	var totalCount uint32 = 0
	for _, room := range rooms {
		color.Green("Room: %s, Participants: %d", room.Name, room.NumParticipants)
		totalCount += room.NumParticipants
	}

	for _, room := range rooms {

		baseURL := "http://" + emqxHost + ":" + emqxPort + "/api/v5/subscriptions?topic=room/%s/listener&limit=1"
		url := fmt.Sprintf(baseURL, room.Name)
		listenerCount, err := fetchURL(url)
		if err != nil {
			fmt.Println("Error fetching listener count", err)
		}
		color.Magenta("Updating Room: %s, Participants: %d | Listeners %d", room.Name, room.NumParticipants, listenerCount)
		updated, err := client.Room.FindUnique(
			db.Room.ID.Equals(room.Name),
		).Update(
			db.Room.ParticipantCount.Set(int(room.NumParticipants)),
			db.Room.ListenerCount.Set(int(listenerCount)),
		).Exec(ctx)
		if err != nil {
			fmt.Println("Error updating room listener", err)
		}
		fmt.Println("Updated Room in DB", updated.ID)
	}
	println("Total Participant Count:", totalCount)
}

func fetchEgress() {
	hostURL := os.Getenv("LIVEKIT_HOST")
	apiKey := os.Getenv("LIVEKIT_API_KEY")
	apiSecret := os.Getenv("LIVEKIT_API_KEY_SECRET")

	egressClient := lksdk.NewEgressClient(hostURL, apiKey, apiSecret)

	egresses, err := egressClient.ListEgress(context.Background(), &livekit.ListEgressRequest{
		Active: true,
	})
	if err != nil {
		fmt.Println("Error fetching eggresses", err)
	}
	// print number of eggresses
	fmt.Println("Number of egresses:", len(egresses.Items))
}

func main() {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file")
	}
	c := color.New(color.FgCyan).Add(color.Underline)
	ctx := context.Background()
	client := db.NewClient()

	defer func() {
		if err := client.Prisma.Disconnect(); err != nil {
			panic(err)
		}
	}()

	client.Connect()
	// Get all live rooms
	// measure time to run
	startTime := time.Now()
	fetchLiveRooms(client, ctx)
	// fetchEgress()
	elapsed := time.Since(startTime)
	c.Printf("Room counts updated in: %s\n", elapsed)

	// Get all egress instances

	// Organize rooms based on type of content being shared
	// 	- Video (camera) -- should have room video composite egress and audio only egress instances
	// 	- Screen (screen) -- should have both screen and audio only egress instances
	// 	- Audio Only (microphone) -- should have audio only egress instances

	// If any of the egress instances are not available, sleep for 5 seconds and try again. If they are still not available, spawn new egress instances.
}

func fetchURL(url string) (int, error) {
	// Create a new HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return 0, err
	}

	// Encode the username and password in Base64
	auth := base64.StdEncoding.EncodeToString([]byte("43c6fffaa75a48fd" + ":" + "TwcB2mve9C35ke2ghHRJ5uRWvAzlFZGqgcowp9BPCZE1A"))
	req.Header.Add("Authorization", "Basic "+auth)

	// Perform the HTTP GET request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error fetching URL:", err)
		return 0, err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return 0, err
	}

	// Unmarshal the JSON response into the Response struct
	var response Response
	err = json.Unmarshal(body, &response)
	if err != nil {
		fmt.Println("Error unmarshalling JSON:", err)
		return 0, err
	}

	// Return value or error
	return response.Meta.Count, nil

}
