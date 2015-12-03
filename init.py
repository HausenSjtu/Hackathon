import os
import redis
import pymysql
import json

redis_host = os.getenv("REDIS_HOST", "localhost")
redis_port = int(os.getenv("REDIS_PORT", "6379"))

r = redis.Redis(host=redis_host, port=redis_port, db=0)

# r.flushall()

conn = pymysql.connect(host=os.getenv("DB_HOST", "localhost"),
                         port=int(os.getenv("DB_PORT", 3306)),
                         user=os.getenv("DB_USER", "root"),
                         passwd=os.getenv("DB_PASS", "toor"),
                         db=os.getenv("DB_NAME", "eleme"))

redis_connectionpool = redis.Redis(connection_pool =
                                    redis.ConnectionPool(host=os.getenv("REDIS_HOST", "localhost"), port=int(os.getenv("REDIS_PORT", 6379))))
# redis_connectionpool = redis.StrictRedis(host=os.getenv("REDIS_HOST", "localhost"), port=int(os.getenv("REDIS_PORT", 6379)))

cur = conn.cursor()
sql = "SELECT * FROM food"
cur.execute(sql)
foodList=[]
food_id_set=set([])
for r in cur:
    d={}
    d['id']=r[0]
    d['stock']=r[1]
    d['price']=r[2]
    food_id_set.add(r[0])
    foodList.append(d)
    # r_food[r[0]]=json.dumps(r[1:])
    redis_connectionpool.hmset("food:"+str(r[0]), d)

local_db = {}

local_db["users"] = {}
local_db["access_token"] = {}
local_db["orders"] = {}
local_db["carts"] = {}
cur.execute("SELECT id, name, password FROM user")
for id, name, pw in cur.fetchall():
    local_db["users"][name] = {"id": id,  "username": name, "password": pw}

foodList=json.dumps(foodList,sort_keys=True)
cur.close()
