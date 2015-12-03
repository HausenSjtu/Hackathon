#!/usr/bin/env python
# -*- coding: utf-8 -*-
from eventlet import wsgi
import eventlet
import json
import os
import pymysql
import redis
import sys
import uuid
import traceback
import collections
from init import *
# import spawning
from multiprocessing import Process

host = os.getenv("APP_HOST", "localhost")
port = int(os.getenv("APP_PORT", "8080"))

get_order_multiply = None
order_multiply = None

# conn = pymysql.connect(host=os.getenv("DB_HOST", "localhost"),
#                          port=int(os.getenv("DB_PORT", 3306)),
#                          user=os.getenv("DB_USER", "root"),
#                          passwd=os.getenv("DB_PASS", "toor"),
#                          db=os.getenv("DB_NAME", "eleme"))


invalid_access_token = json.dumps({"code": "INVALID_ACCESS_TOKEN","message": u"无效的令牌"})
empty_request = json.dumps({"code" : "EMPTY_REQUEST", "message": u"请求体为空"})
malformed_json = json.dumps({"code": "MALFORMED_JSON","message": u"格式错误"})
cart_not_found = json.dumps({"code": "CART_NOT_FOUND","message": u"篮子不存在"})
not_authorized_to_access_cart = json.dumps({"code": "NOT_AUTHORIZED_TO_ACCESS_CART","message": u"无权限访问指定的篮子"})
food_out_of_limit = json.dumps({"code": "FOOD_OUT_OF_LIMIT","message": u"篮子中食物数量超过了三个"})
food_not_found = json.dumps({"code": "FOOD_NOT_FOUND","message": u"食物不存在"})
food_out_of_stock = json.dumps({"code": "FOOD_OUT_OF_STOCK","message": u"食物库存不足"})
not_authorized_to_access_cart = json.dumps({"code": "NOT_AUTHORIZED_TO_ACCESS_CART","message": u"无权限访问指定的订单"})
order_out_of_limit = json.dumps({"code": "ORDER_OUT_OF_LIMIT","message": u"每个用户只能下一单"})
empty_order = json.dumps([])

def access_token_verify(env):
    try :
        access_token = env['QUERY_STRING'].split('=')[1]
    except Exception, e:
        try:
            access_token = env['HTTP_ACCESS_TOKEN']
        except Exception, e:
            return ('401', [('Content-Type', 'text/plain')], invalid_access_token)

    try:
        if access_token == None or len(access_token) == 0:
            return ('401', [('Content-Type', 'text/plain')], invalid_access_token)
    except Exception, e:
        info = sys.exc_info()
        print info[0],":",info[1]
        print traceback.format_exc()

    return (access_token,)

def body_verify(env):
    msg = env['eventlet.input'].read(env['CONTENT_LENGTH'])

    if len(msg) == 0:
        return ('400', [('Content-Type', 'text/plain')], empty_request)

    try:
        msg = json.loads(msg)
        return (msg, )
    except Exception, e:
        return ('400', [('Content-Type', 'text/plain')], malformed_json)

def order_verify(access_token):
    order = local_db["orders"].get(access_token)
    if order:
        return order
    else:
        order = redis_connectionpool.hexists("orders", access_token + ":cart_id")
        local_db["orders"][access_token] = order
        return order

# def set_o

def set_user_token(username):
    global local_db
    users = local_db["users"]
    access_token = users[username].get("access_token")

    if not access_token:
        access_token = uuid.uuid4().hex
        id = users[username]["id"]
        redis_connectionpool.hset("users", access_token, id)
        local_db["access_token"][access_token] = username

    return access_token

def get_userid_by_token(access_token):
    id = redis_connectionpool.hget("users", access_token)
    return id


def login(env, start_response):
    res = body_verify(env)
    if len(res) > 1:
        http_code, content_type, msg = res
        start_response(http_code, content_type)
        return msg
    else:
        msgLogin = res[0]

    try:
        username = msgLogin['username']
        users = local_db["users"]
        if not users[username]:
            return "USER_AUTH_FAIL"
        else:
            if not users.get(username) or users[username]['password'] == msgLogin['password']:
                id = users[username]["id"]
                access_token = set_user_token(username)

                start_response('200', [('Content-Type', 'text/plain')])
                return json.dumps({'user_id': users[username]["id"], 'username':username, 'access_token':access_token})
            else:
                start_response('403', [('Content-Type', 'text/plain')])
                return json.dumps({"code": "USER_AUTH_FAIL","message": u"用户名或密码错误"})

    except Exception, e:
        info = sys.exc_info()
        print info[0],":",info[1]
        print traceback.format_exc()


def foods(env, start_response):
    res = access_token_verify(env)
    if len(res) > 1:
        http_code, content_type, msg = res
        start_response(http_code, content_type)
        return msg
    else:
        start_response('200', [('Content-Type', 'text/plain')])
        return foodList



def add_cart(access_token):
    cart_id = uuid.uuid4().hex
    # cart = json.dumps({'user_id': local_db["users"][local_db["access_token"][access_token]]["id"], 'food_info':{}, 'total':0})
    # cart = {'access_token': access_token, 'food_info':{}, 'total':0}
    cart = {'access_token': access_token, 'total':0}
    cart_key = "carts:" + cart_id
    redis_connectionpool.hmset(cart_key, cart)
    local_db[cart_key] = cart

    return cart_id

def get_cart(cart_id):
    global local_db
    cart_key = "carts:" + cart_id
    cart = local_db.get(cart_key)
    if not cart:
        cart = redis_connectionpool.hgetall(cart_key)
        local_db[cart_key] = cart
    return cart

def carts(env, start_response):
    res = access_token_verify(env)
    if len(res) > 1:
        http_code, content_type, msg = res
        start_response(http_code, content_type)
        return msg
    else:
        access_token = res[0]
        cart_id = add_cart(access_token)
        d = {'cart_id': cart_id}
        start_response('200', [('Content-Type', 'text/plain')])
        return json.dumps(d)

def user_verify(access_token, id):
    pass

def db_add_food(cart_id, food_id, count):
    pipe = redis_connectionpool.pipeline()
    pipe.hincrby("carts:foods:"+cart_id, food_id, count).hincrby("carts:"+cart_id, 'total', count).execute()

def add_food(env, start_response):
    global local_db

    res = body_verify(env)
    if len(res) > 1:
        http_code, content_type, msg = res
        start_response(http_code, content_type)
        return msg
    else:
        msgPatch = res[0]

    res = access_token_verify(env)
    if len(res) > 1:
        http_code, content_type, msg = res
        start_response(http_code, content_type)
        return msg
    else:
        access_token = res[0]
        cart_id = env['PATH_INFO'].split('/')[2]
        food_id = msgPatch['food_id']
        count = msgPatch['count']

        cart = get_cart(cart_id)

        if cart:
            if cart['access_token'] == access_token:
                foodNum = count
                if foodNum + int(cart['total']) > 3:
                    start_response('403', [('Content-Type', 'text/plain')])
                    return food_out_of_limit

                global food_id_set
                if not food_id in food_id_set:
                    start_response('404', [('Content-Type', 'text/plain')])
                    return food_not_found

                db_add_food(cart_id, food_id, count)
                start_response('204', [('Content-Type', 'text/plain')])
                return json.dumps({})
            else:
                start_response('403', [('Content-Type', 'text/plain')])
                return not_authorized_to_access_cart

        else:
            start_response('404', [('Content-Type', 'text/plain')])
            return cart_not_found


def get_order(env, start_response):
    res = access_token_verify(env)
    if len(res) > 1:
        http_code, content_type, msg = res
        start_response(http_code, content_type)
        return msg
    else:
        access_token = res[0]

        order = order_verify(access_token)
        # if r_id_acc.exists(access_token):
        #     myId = str(json.loads(r_id_acc[access_token]))
        if order:
            get_order_lua = """
            local access_token = KEYS[1]
            local cart_id = redis.call("HGET", "orders", access_token .. ":cart_id")
            local id = redis.call("HGET", "orders", access_token .. ":id")
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

            local result = {id = id, user_id = id, items = items, total = total}
            return cjson.encode({result});
            """

            global get_order_multiply
            if not get_order_multiply:
                get_order_multiply = redis_connectionpool.register_script(get_order_lua)
            start_response('200', [('Content-Type', 'text/plain')])
            return get_order_multiply(keys=[access_token], args=[])
        #         myOrder = json.loads(r_orders[myId])
        #         items=[]
        #         for food_id in myOrder:
        #             if food_id=='_zhs_total_':
        #                 total = myOrder[food_id]
        #             else:
        #                 d = collections.OrderedDict()
        #                 d['food_id']=int(food_id)
        #                 d['count']=myOrder[food_id]
        #                 items.append(d)
        #         reOrder = collections.OrderedDict()
        #         reOrder['id'] = myId
        #         reOrder['items'] = items
        #         reOrder['total'] = total
        #         start_response('200', [('Content-Type', 'text/plain')])
                #return json.dumps([reOrder])
            # else:
            #     start_response('401', [('Content-Type', 'text/plain')])
            #     return not_authorized_to_access_cart
        else:
            start_response('200', [('Content-Type', 'text/plain')])
            return empty_order


def order(env, start_response):
    res = body_verify(env)
    if len(res) > 1:
        http_code, content_type, msg = res
        start_response(http_code, content_type)
        return msg
    else:
        msgOrder = res[0]

    res = access_token_verify(env)
    if len(res) > 1:
        http_code, content_type, msg = res
        start_response(http_code, content_type)
        return msg

    access_token = res[0]
    cart_id = msgOrder['cart_id']
    cart = get_cart(cart_id)

    if cart:
        if cart['access_token'] == access_token:
            if order_verify(access_token):
                start_response('403', [('Content-Type', 'text/plain')])
                return order_out_of_limit
            else:
                # d = cart['food_info']
                # if not d:
                #     start_response('403', [('Content-Type', 'text/plain')])
                #     return food_out_of_stock
                # stock={}
                # price={}

                # pipe = r_food.pipeline()
                # while 1:
                #     try:
                #         for food_id in d:
                #             pipe.watch(food_id)
                #             r_food_get = json.loads(r_food[food_id])

                #             stock[food_id], price[food_id] = r_food_get
                #             if stock[food_id] < d[food_id]:
                #                 start_response('403', [('Content-Type', 'text/plain')])
                #                 return food_out_of_stock
                #         pipe.multi()

                #         for food_id in d:
                #             r_food_get=[0,0]
                #             stock[food_id]-=d[food_id]
                #             r_food_get[0] = stock[food_id]
                #             r_food_get[1] = price[food_id]
                #             pipe.set(food_id,json.dumps(r_food_get))
                #         pipe.execute()
                #         break

                #     except redis.WatchError:
                #         continue
                #     finally:
                #         pipe.reset()
                order_lua = """
                local cart_id = KEYS[1]
                local foods_kv = redis.call("HGETALL", "carts:foods:"..cart_id)
                local foods = {}
                local food
                local count = 0

                for idx = 1, #foods_kv, 2 do
                    food = foods_kv[idx]
                    count = tonumber(foods_kv[idx + 1])
                    foods[food] = count
                    if count > tonumber(redis.call("HGET", "food:".. food, "stock")) then
                        return nil
                    end
                end

                for f, c in pairs(foods) do
                    redis.call("HINCRBY", "food:" .. f, "stock", -c)
                end

                return 1
                """

                if not order_multiply:
                    multiply = redis_connectionpool.register_script(order_lua)
                result = multiply(keys=[cart_id], args=[])

                if not result:
                    start_response('403', [('Content-Type', 'text/plain')])
                    return food_out_of_stock

                id = get_userid_by_token(access_token)
                redis_connectionpool.hmset("orders", {access_token+":cart_id": cart_id, access_token+":id": id})
                start_response('200', [('Content-Type', 'text/plain')])
                return json.dumps({"id": id})
        else:
            start_response('403', [('Content-Type', 'text/plain')])
            return not_authorized_to_access_cart
    else:
        start_response('404', [('Content-Type', 'text/plain')])
        return cart_not_found


def root(env, start_response):
    start_response('200 OK', [('Content-Type', 'text/plain')])
    return ['Welcome!\r\n']

def error(env, start_response):
    start_response('404 Not Found', [('Content-Type', 'text/plain')])
    return ['Not Found\r\n']

methods = {'': root,
           'loginPOST': login,
           'foodsGET': foods,
           'cartsPOST': carts,
           'cartsPATCH': add_food,
           'ordersGET': get_order,
           'ordersPOST': order}

def app(env, start_response):
    method = methods.get(env['PATH_INFO'].split("/")[1] + env['REQUEST_METHOD'], error)

    return method(env, start_response)


if __name__ == '__main__':
    eventlet.patcher.monkey_patch(all=False, socket=True, time=True,
                          select=True, thread=True, os=True)
    # wsgi.server.log_message = lambda self,message:None
    wsgi.server(eventlet.listen((host, port)), app, log = None, max_size = 2000, log_output = False)
    # eventlet_listen = eventlet.listen((host, port))
    # pool = multiprocessing.Pool(processes=4)
    # for i in xranges(4):
    #     pool.apply_async(wsgi.server, (eventlet_listen, app, log = None, max_size = 2000, log_output = False))
    #     pool.apply_async(wsgi.server, (eventlet_listen, app))
    # pool.close()
    # pool.join()
