package main

import (
	// "net/http"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/garyburd/redigo/redis"
	_ "github.com/go-sql-driver/mysql"
	"github.com/labstack/echo"
	"github.com/satori/go.uuid"
	"net/http"
	"os"
	"time"
	// "io/ioutil"
	// "math/rand"
	"runtime"
	"strconv"
	// "sync"
	// "golang.org/x/net/websocket"
)

type UserInfo struct {
	UserId   int
	Password string
}

type User struct {
	Username string
	Password string
}

type FoodInfoDet struct {
	FoodId    int
	FoodCount int
}

type FoodInfoItems struct {
	FoodID int `json:"food_id"`
	Count  int `json:"count"`
}

type CartDescription struct {
	UserId   int
	FoodInfo []FoodInfoDet
	Total    int
}

type CartReq struct {
	FoodId int `json:"food_id"`
	Count  int `json:"count"`
}

type OrderReq struct {
	CartId string `json:"cart_id"`
}

type OrderInfo struct {
	OrderId string          `json:"id"`
	Items   []FoodInfoItems `json:"items"`
	Total   int             `json:"total"`
}

type OrderInfoAdmin struct {
	ID     string          `json:"id"`
	UserID int             `json:"user_id"`
	Items  []FoodInfoItems `json:"items"`
	Total  int             `json:"total"`
}

var db *sql.DB
var redisClient *redis.Pool

var userList map[string]UserInfo

// var accessTokenId map[string]int
var foodIdPrice map[int]int
var foodList []map[string]int
var foodListBytes []byte
var foodLength int
var AccessTokens []string
var accessTokensId map[string]int

var orderIdCache [][]byte
var userInfoCache [][]byte

// redis lua
var get_order_lua_sha string
var order_lua_sha string
var admin_order_lua_sha string
var set_accesstoken_sha string

var EMPTY_REQUEST, _ = json.Marshal(map[string]string{"code": "EMPTY_REQUEST", "message": "请求体为空"})
var MALFORMED_JSON, _ = json.Marshal(map[string]string{"code": "MALFORMED_JSON", "message": "格式错误"})
var USER_AUTH_FAIL, _ = json.Marshal(map[string]string{"code": "USER_AUTH_FAIL", "message": "用户名或密码错误"})
var INVALID_ACCESS_TOKEN, _ = json.Marshal(map[string]string{"code": "INVALID_ACCESS_TOKEN", "message": "无效的令牌"})
var CART_NOT_FOUND, _ = json.Marshal(map[string]string{"code": "CART_NOT_FOUND", "message": "篮子不存在"})
var ORDER_OUT_OF_LIMIT, _ = json.Marshal(map[string]string{"code": "ORDER_OUT_OF_LIMIT", "message": "每个用户只能下一单"})
var FOOD_OUT_OF_LIMIT, _ = json.Marshal(map[string]string{"code": "FOOD_OUT_OF_LIMIT", "message": "篮子中食物数量超过了三个"})
var FOOD_NOT_FOUND, _ = json.Marshal(map[string]string{"code": "FOOD_NOT_FOUND", "message": "食物不存在"})
var NOT_AUTHORIZED_TO_ACCESS_CART, _ = json.Marshal(map[string]string{"code": "NOT_AUTHORIZED_TO_ACCESS_CART", "message": "无权限访问指定的篮子"})
var FOOD_OUT_OF_STOCK, _ = json.Marshal(map[string]string{"code": "FOOD_OUT_OF_STOCK", "message": "食物库存不足"})
var EMPTY, _ = json.Marshal(map[string]string{})

// var u = fmt.Sprintf("%s", uuid.NewV1())
// var r1 = rand.New(rand.NewSource(time.Now().UnixNano()))

// var r2 = rand.New(rand.NewSource(time.Now().UnixNano()))

// func create_uuid() string {
// s := u + strconv.Itoa(r1.Intn(50000)) + strconv.Itoa(r2.Intn(50000))
// fmt.Println(s)
// return s
// return u + strconv.Itoa(r1.Intn(50000)) + strconv.Itoa(r2.Intn(50000))
// return u + strconv.Itoa(r1.Int())
// }

func cache() {
	//cache the food table to local and redis
	foodIdPrice = make(map[int]int)
	accessTokensId = make(map[string]int)

	rc := redisClient.Get()
	rows, _ := db.Query("SELECT * FROM food")
	var foodId int
	var foodStock int
	var foodPrice int
	for rows.Next() {
		rows.Columns()
		rows.Scan(&foodId, &foodStock, &foodPrice)
		foodsamp := map[string]int{
			"id":    foodId,
			"stock": foodStock,
			"price": foodPrice,
		}
		foodList = append(foodList, foodsamp)
		foodIdPrice[foodId] = foodPrice
		rc.Do("HMSET", "food:"+strconv.Itoa(foodId), "stock", foodStock, "price", foodPrice)
	}
	foodLength = len(foodList)
	foodListBytes, _ = json.Marshal(foodList)
	// fmt.Println(u)

	//cache the user table to local and redis
	userList = make(map[string]UserInfo)
	rows, _ = db.Query("SELECT * FROM user")
	var userId int
	var userName string
	var userPassword string
	var userIdName []string
	userIdName = append(userIdName, "where db happens")
	AccessTokens = append(AccessTokens, "where db happens")

	for rows.Next() {
		rows.Columns()
		rows.Scan(&userId, &userName, &userPassword)
		userList[userName] = UserInfo{userId, userPassword}
		accessToken := fmt.Sprintf("%s", uuid.NewV1())
		AccessTokens = append(AccessTokens, accessToken)

		userIdName = append(userIdName, userName)
	}
	var ret []string
	AccessTokensJson, _ := json.Marshal(AccessTokens)
	retJson, _ := redis.Bytes(rc.Do("EVALSHA", set_accesstoken_sha, 1, AccessTokensJson))
	json.Unmarshal(retJson, &ret)

	if ret[0] != "ok" {
		copy(AccessTokens, ret)
	}

	for i, v := range AccessTokens {
		accessTokensId[v] = i

		infoMap := map[string]interface{}{"user_id": i, "username": userIdName[i], "access_token": v}
		infoMapJson, _ := json.Marshal(infoMap)
		userInfoCache = append(userInfoCache, infoMapJson)

		idMap := map[string]string{"id": strconv.Itoa(i)}
		idMapJson, _ := json.Marshal(idMap)
		orderIdCache = append(orderIdCache, idMapJson)
	}

	// map[string]interface{}{"user_id": v.UserId, "username": newUser.Username, "access_token": AccessTokens[v.UserId]}
	// map[string]string{"id": strconv.Itoa(id)}
	rc.Close()
}

func luaCache() {
	rc := redisClient.Get()

	set_accesstoken_lua := `
        local a = redis.call("GET","all_accesstokens")
        if a==false then
            redis.call("SET","all_accesstokens",KEYS[1])
            return cjson.encode({"ok"})
        end
        return a
    `
	set_accesstoken_sha, _ = redis.String(rc.Do("script", "load", set_accesstoken_lua))

	order_lua := `
		local cart_id = KEYS[1]
		local access_token = KEYS[2]

		if redis.call("HEXISTS", "orders", "cart_id:"..access_token) == 1 then
			return -4
		end

		local real_access_token = redis.call("HGET", "carts:".. cart_id, "access_token")
		if not real_access_token then
			return -3
		end
		if real_access_token ~= access_token then
			return -2
		end

		local foods_kv = redis.call("HGETALL", "carts:foods:"..cart_id)
		local foods = {}
		local food
		local count = 0

		for idx = 1, #foods_kv, 2 do
			food = foods_kv[idx]
			count = tonumber(foods_kv[idx + 1])
			foods[food] = count
			if count > tonumber(redis.call("HGET", "food:".. food, "stock")) then
				return -1
			end
		end

		for f, c in pairs(foods) do
			redis.call("HINCRBY", "food:" .. f, "stock", -c)
		end

		local id = KEYS[3]
		redis.call("HMSET", "orders", "cart_id:" .. access_token, cart_id, "id:".. access_token, id)
		redis.call("LPUSH", "finish_orders", access_token)
		redis.call("HSET", "carts:"..cart_id, "done", 1)

		return id
		`

	order_lua_sha, _ = redis.String(rc.Do("script", "load", order_lua))

	get_order_lua := `
		local access_token = KEYS[1]
		local cart_id = redis.call("HGET", "orders", "cart_id:" .. access_token)
		if not cart_id then
			return cjson.encode({});
		end


		local id = redis.call("HGET", "orders", "id:" .. access_token)
		local items = {}
		local foods_kv = redis.call("HGETALL", "carts:foods:"..cart_id)
		local total = 0


		local i = 1
		local food, count
		for idx = 1, #foods_kv, 2 do
			food = foods_kv[idx]
			count = tonumber(foods_kv[idx + 1])
			items[i] = {food_id = tonumber(food), count = count}
			i = i+ 1
			total = total + tonumber(redis.call("HGET", "food:".. food, "price")) * count
		end

		local result = {id = id, user_id = tonumber(id), items = items, total = total}
		return cjson.encode({result});
		`

	get_order_lua_sha, _ = redis.String(rc.Do("script", "load", get_order_lua))

	admin_order_lua := `
		-- local access_token = KEYS[1]
		-- local cart_id = redis.call("HGET", "orders", access_token .. ":cart_id")
		-- if not cart_id then
		-- 	return cjson.encode({});
		-- end
		local access_token
		local access_tokens = redis.call("LRANGE", "finish_orders", 0, -1)
		local result={}

		for access_token_idx = 1, #access_tokens do
			access_token = access_tokens[access_token_idx]
			local id = redis.call("HGET", "orders", "id:" .. access_token)
			local items = {}
			local cart_id = redis.call("HGET", "orders", "cart_id:" .. access_token)
			local foods_kv = redis.call("HGETALL", "carts:foods:"..cart_id)
			local total = 0


			local i = 1
			local food, count
			for idx = 1, #foods_kv, 2 do
				food = foods_kv[idx]
				count = tonumber(foods_kv[idx + 1])
				items[i] = {food_id = tonumber(food), count = count}
				i = i+ 1
				total = total + tonumber(redis.call("HGET", "food:".. food, "price")) * count
			end

			result[access_token_idx] = {id = id, user_id = tonumber(id), items = items, total = total}
		end


		return cjson.encode(result);
		`

	admin_order_lua_sha, _ = redis.String(rc.Do("script", "load", admin_order_lua))

	rc.Close()
}

//init - connect database
func init() {
	//connect the mysql server
	sqlHost := os.Getenv("DB_HOST")
	sqlPort := os.Getenv("DB_PORT")
	if sqlHost == "" {
		sqlHost = "localhost"
	}
	if sqlPort == "" {
		sqlPort = "3306"
	}
	db, _ = sql.Open("mysql", "root:toor@tcp("+sqlHost+":"+sqlPort+")/eleme")
	db.SetMaxOpenConns(2000)
	db.SetMaxIdleConns(1000)
	db.Ping()

	//connect the redis server
	redisHost := os.Getenv("REDIS_HOST")
	redisPort := os.Getenv("REDIS_PORT")
	if redisHost == "" {
		redisHost = "localhost"
	}
	if redisPort == "" {
		redisPort = "6379"
	}

	redisClient = &redis.Pool{
		MaxIdle:     650,
		MaxActive:   6500,
		IdleTimeout: 60 * time.Second,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", redisHost+":"+redisPort)
			if err != nil {
				return nil, err
			}
			c.Do("SELECT", 0)
			return c, nil
		},
	}

	rc := redisClient.Get()

	//rc.Do("FLUSHDB")

	luaCache()

	cache()

	rc.Close()

	fmt.Println("Init End")
}

func getAccessToken(req *http.Request) string {
	req.ParseForm()
	var accessToken string
	if len(req.Form) != 0 {
		accessToken = req.Form["access_token"][0]
	} else {
		accessToken = req.Header.Get("Access-Token")
	}
	return accessToken
}

func checkerror(err error) {
}

// Handler
func welcome(c *echo.Context) error {
	return c.String(http.StatusOK, "Welcome!\n")
}

func postLogin(c *echo.Context) error {
	if c.Request().ContentLength == 0 {
		return c.Bytes(400, EMPTY_REQUEST)
	}

	newUser := new(User)
	err := c.Bind(newUser)
	if err == nil {
		if v, ok := userList[newUser.Username]; ok {
			if newUser.Password == v.Password {
				// accessToken := fmt.Sprintf("%s", uuid.NewV1())
				// // accessToken := create_uuid()

				// // @db
				// rc := redisClient.Get()
				// defer rc.Close()
				// rc.Do("SET", "user:"+accessToken, v.UserId)

				// return c.JSON(200, map[string]interface{}{"user_id": v.UserId, "username": newUser.Username, "access_token": AccessTokens[v.UserId]})
				return c.Bytes(200, userInfoCache[v.UserId])
			}
			return c.Bytes(403, USER_AUTH_FAIL)
		}
		return c.Bytes(403, USER_AUTH_FAIL)
	}
	return c.Bytes(400, MALFORMED_JSON)
}

func getFoods(c *echo.Context) error {
	accessToken := getAccessToken(c.Request())

	if len(accessToken) == 0 {
		return c.Bytes(401, INVALID_ACCESS_TOKEN)
	}

	// rc := redisClient.Get()
	// defer rc.Close()
	// if ok, _ := redis.Int(rc.Do("EXISTS", "user:"+accessToken)); ok == 1 {
	// 	return c.JSON(200, foodList)
	// }

	// return c.JSON(401, map[string]string{"code": "INVALID_ACCESS_TOKEN", "message": "无效的令牌"})
	userId := accessTokensId[accessToken]
	if userId == 0 {
		return c.Bytes(401, INVALID_ACCESS_TOKEN)
	}

	return c.BytePointer(200, &foodListBytes)
}

func postCarts(c *echo.Context) error {
	accessToken := getAccessToken(c.Request())
	if len(accessToken) == 0 {
		return c.Bytes(401, INVALID_ACCESS_TOKEN)
	}

	cartUid := fmt.Sprintf("%s", uuid.NewV1())
	// cartUid := create_uuid()
	cartKey := "carts:" + cartUid

	// @db
	//cart = {'access_token': access_token, 'total':0}
	rc := redisClient.Get()
	defer rc.Close()
	rc.Do("HMSET", cartKey, "access_token", accessToken, "total", 0, "done", 0)

	return c.JSON(200, map[string]string{"cart_id": cartUid})
}

func patchCarts(c *echo.Context) error {
	if c.Request().ContentLength == 0 {
		return c.Bytes(400, EMPTY_REQUEST)
	}

	accessToken := getAccessToken(c.Request())
	if len(accessToken) == 0 {
		return c.Bytes(401, INVALID_ACCESS_TOKEN)
	}

	rc := redisClient.Get()
	defer rc.Close()

	cartIdReq := c.Param("cart_id")
	// @log
	// fmt.Println(cartIdReq)

	if cart, err := redis.StringMap(rc.Do("HGETALL", "carts:"+cartIdReq)); err != nil || len(cart) == 0 {
		return c.Bytes(404, CART_NOT_FOUND)
	} else {
		// @log
		// for key, value := range cart {
		// 	fmt.Println(key, value)
		// }

		newCart := new(CartReq)
		err := c.Bind(newCart)
		if err == nil {
			foodId := newCart.FoodId
			count := newCart.Count

			if accessToken == cart["access_token"] {
				if cart["done"] == "1" {
					return c.Bytes(403, ORDER_OUT_OF_LIMIT)
				}
				total, _ := strconv.Atoi(cart["total"])
				if count+total > 3 {
					return c.Bytes(403, FOOD_OUT_OF_LIMIT)
				}

				if _, err := foodIdPrice[foodId]; !err {
					return c.Bytes(404, FOOD_NOT_FOUND)
				}

				// pipe.hincrby("carts:foods:"+cart_id, food_id, count).hincrby("carts:"+cart_id, 'total', count).execute()
				// @db
				// fmt.Println("carts:foods:"+cartIdReq, foodId, count)
				rc.Send("HINCRBY", "carts:foods:"+cartIdReq, foodId, count)
				rc.Send("HINCRBY", "carts:"+cartIdReq, "total", count)
				rc.Flush()
				rc.Receive()
				rc.Receive()

				// @log
				// fmt.Println("carts:foods:"+cartIdReq, foodId, count)

				return c.Bytes(204, EMPTY)
			}

			return c.Bytes(401, NOT_AUTHORIZED_TO_ACCESS_CART)
		}

		return c.Bytes(400, MALFORMED_JSON)
	}
}

func postOrders(c *echo.Context) error {
	accessToken := getAccessToken(c.Request())
	if len(accessToken) == 0 {
		return c.Bytes(401, INVALID_ACCESS_TOKEN)
	}

	if c.Request().ContentLength == 0 {
		return c.Bytes(400, EMPTY_REQUEST)
	}

	newOrder := new(OrderReq)
	err := c.Bind(newOrder)
	if err == nil {
		cartId := newOrder.CartId
		rc := redisClient.Get()
		defer rc.Close()
		// fmt.Println(accessToken, cartId)

		// @db
		// id, _ := redis.Int(order_lua_rc.Do(rc, cartId, accessToken))
		id, _ := redis.Int(rc.Do("EVALSHA", order_lua_sha, 3, cartId, accessToken, accessTokensId[accessToken]))
		switch id {
		case -1:
			return c.Bytes(403, FOOD_OUT_OF_STOCK)
		case -2:
			// -2, CART_NOT_FOUND
			return c.Bytes(401, NOT_AUTHORIZED_TO_ACCESS_CART)
		case -3:
			return c.Bytes(404, CART_NOT_FOUND)
		case -4:
			return c.Bytes(403, ORDER_OUT_OF_LIMIT)
		default:
			// return c.JSON(200, map[string]string{"id": strconv.Itoa(id)})
			return c.Bytes(200, orderIdCache[id])
		}
	}
	return c.Bytes(400, MALFORMED_JSON)
}

func getOrders(c *echo.Context) error {
	accessToken := getAccessToken(c.Request())

	if len(accessToken) == 0 {
		return c.Bytes(401, INVALID_ACCESS_TOKEN)
	}

	rc := redisClient.Get()
	defer rc.Close()

	// @db
	// result, _ := redis.String(rc.Do("EVAL", get_order_lua, 1, accessToken))
	// result, _ := redis.String(get_order_lua_rc.Do(rc, accessToken))
	userId := accessTokensId[accessToken]
	if userId == 0 {
		return c.Bytes(401, INVALID_ACCESS_TOKEN)
	}

	result, _ := redis.String(rc.Do("EVALSHA", get_order_lua_sha, 1, accessToken))
	// @log
	// fmt.Println("Haha", accessToken, result)
	return c.String(http.StatusOK, result)
}

func getAdminOrders(c *echo.Context) error {
	accessToken := getAccessToken(c.Request())
	if len(accessToken) == 0 {
		return c.Bytes(401, INVALID_ACCESS_TOKEN)
	}

	rc := redisClient.Get()
	defer rc.Close()
	// if ok, _ := redis.Int(rc.Do("EXISTS", "user:"+accessToken)); ok == 1 {
	// @db
	// result, _ := redis.String(admin_order_lua_rc.Do(rc))
	userId := accessTokensId[accessToken]
	if userId == 0 {
		return c.Bytes(401, INVALID_ACCESS_TOKEN)
	}

	result, _ := redis.String(rc.Do("EVALSHA", admin_order_lua_sha, 0))
	return c.String(http.StatusOK, result)
	// }

	// return c.JSON(401, map[string]string{"code": "NOT_AUTHORIZED_TO_ACCESS_CART", "message": "无权限访问指定的订单"})
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	//get the environment
	host := os.Getenv("APP_HOST")
	port := os.Getenv("APP_PORT")
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "8080"
	}

	// Echo instance
	e := echo.New()

	// Routes
	e.Get("/", welcome)
	e.Post("/login", postLogin)
	e.Get("/foods", getFoods)
	e.Post("/carts", postCarts)
	e.Patch("/carts/:cart_id", patchCarts)
	e.Post("/orders", postOrders)
	e.Get("/orders", getOrders)
	e.Get("/admin/orders", getAdminOrders)
	// Start server
	addr := fmt.Sprintf("%s:%s", host, port)
	e.Run(addr)
}
