#!/usr/bin/env python3
"""Seed a local account with one full day, for looking at My Day.

Creates an account through the API, then fills today: meals at meal times,
water through the day, a staged night of sleep from "Apple Health", activity
totals, a workout, a weight, and the day rules that draw the dashed markers.

Local development only. Meal and water times are moved with SQL afterwards,
because the API stamps them "now" — pass the psql command that reaches your
database, e.g.:

    PSQL="docker compose exec -T postgres psql -U north -d north" \
        python3 scripts/seed-my-day.py

Prints the email and password to sign in with.
"""
import datetime as dt
import json
import os
import shlex
import subprocess
import time
import urllib.request

BASE = os.environ.get("NORTH_API", "http://localhost:8090/api/v1")
PSQL = shlex.split(os.environ.get("PSQL", "docker compose exec -T postgres psql -U north -d north"))
TZ = os.environ.get("SEED_TZ", "UTC")


def call(method, path, token=None, body=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, method=method, data=data, headers=headers)
    with urllib.request.urlopen(req, timeout=60) as r:
        raw = r.read().decode()
        return json.loads(raw) if raw.strip()[:1] in "{[" else raw


def sql(statement):
    subprocess.run(PSQL + ["-v", "ON_ERROR_STOP=1", "-q", "-c", statement], check=True)


email, password = f"myday+{int(time.time())}@example.com", "Correct-horse-9"
token = call("POST", "/auth/signup", body={
    "email": email, "password": password, "passwordConfirmation": password,
    "displayName": "Alex", "timezone": TZ,
})["token"]
call("POST", "/onboarding", token, {
    "focusAreas": ["fitness", "sleep", "habits"], "coachingStyle": "supportive",
    "nearTermGoal": "Lose four kilos before Christmas",
})
user = call("GET", "/me", token)["user"]["id"]

today = dt.datetime.now(dt.timezone.utc).date()
midnight = dt.datetime.combine(today, dt.time(0, 0), dt.timezone.utc)


def at(h, m=0):
    return (midnight + dt.timedelta(hours=h, minutes=m)).isoformat().replace("+00:00", "Z")


# The body and a macro goal, so the food card has something to compare to.
call("PUT", "/calculator/biometrics", token, {"weightKg": 83.9, "heightCm": 181, "dateOfBirth": "1990-04-02", "sex": "male"})
call("POST", "/calculator/plan", token, {"activityLevel": "moderate", "goal": "cutting", "macroSplit": "moderate_carb"})

# Food: three ingredients at meal times.
meals = [("oat", 80, 8, 10), ("chicken", 180, 13, 5), ("salmon", 200, 21, 0)]
for name, grams, h, m in meals:
    found = call("GET", f"/nutrition/ingredients?q={name}", token)
    items = found.get("ingredients") if isinstance(found, dict) else found
    if not items:
        continue
    call("POST", "/nutrition/log/ingredients", token, {"ingredientId": items[0]["id"], "quantityGrams": grams})
    sql(f"update food_logs set logged_at = '{at(h, m)}' where id = (select id from food_logs where user_id = '{user}' order by logged_at desc limit 1);")

# Water through the day.
for ml, h, m in [(500, 7, 40), (330, 10, 5), (250, 11, 22), (500, 13, 10)]:
    call("POST", "/care/water", token, {"amountMl": ml})
    sql(f"update hydration_logs set logged_at = '{at(h, m)}' where id = (select id from hydration_logs where user_id = '{user}' order by logged_at desc limit 1);")

# A staged night and the day's device totals, as the iOS sync sends them.
blocks = [
    ("sleep_core", -1, 0, 0, 20), ("sleep_deep", 0, 20, 1, 30), ("sleep_core", 1, 30, 2, 40),
    ("sleep_rem", 2, 40, 3, 30), ("sleep_awake", 3, 30, 3, 45), ("sleep_core", 3, 45, 5, 0),
    ("sleep_deep", 5, 0, 5, 30), ("sleep_rem", 5, 30, 6, 40), ("sleep_awake", 6, 40, 6, 50),
]
readings = []
for metric, sh, sm, eh, em in blocks:
    start, end = midnight + dt.timedelta(hours=sh, minutes=sm), midnight + dt.timedelta(hours=eh, minutes=em)
    readings.append({"metric": metric, "value": (end - start).seconds / 60, "unit": "min",
                     "startedAt": start.isoformat().replace("+00:00", "Z"), "endedAt": end.isoformat().replace("+00:00", "Z")})
for metric, value, unit in [("active_calories", 501, "kcal"), ("exercise_minutes", 62, "min"), ("stand_hours", 9, "count"),
                            ("time_in_daylight", 84, "min"), ("steps", 9120, "count")]:
    readings.append({"metric": metric, "value": value, "unit": unit, "startedAt": at(0), "endedAt": at(24)})
for d in range(1, 15):
    day = midnight - dt.timedelta(days=d)
    readings.append({"metric": "hrv_sdnn", "value": 48 + d % 5, "unit": "ms", "startedAt": day.isoformat().replace("+00:00", "Z")})
    readings.append({"metric": "resting_heart_rate", "value": 58 + d % 3, "unit": "count/min", "startedAt": day.isoformat().replace("+00:00", "Z")})
readings.append({"metric": "hrv_sdnn", "value": 55, "unit": "ms", "startedAt": at(6)})
readings.append({"metric": "resting_heart_rate", "value": 57, "unit": "count/min", "startedAt": at(6)})
call("POST", "/health/samples", token, {"readings": readings, "workouts": []})

# Two workouts.
call("POST", "/activity/log", token, {"activityCode": "walking_moderate", "startedAt": at(9, 20), "durationMinutes": 31})
call("POST", "/activity/log", token, {"activityCode": "running_9_8kmh", "startedAt": at(6, 55), "durationMinutes": 31})

# The rules behind the dashed markers.
for kind, when in [("caffeine_cutoff", "15:00"), ("kitchen_closes", "20:00"), ("last_drink", "21:30"), ("screens_off", "22:00")]:
    call("PUT", f"/day/rules/{kind}", token, {"at": when, "enabled": True})

# Caffeine through the morning, supplements with breakfast.
for preset, h, m in [("coffee", 9, 58), ("espresso", 13, 40)]:
    call("POST", "/caffeine", token, {"preset": preset, "loggedAt": at(h, m)})
call("POST", "/supplements", token, {"preset": "omega3", "count": 3})
call("POST", "/supplements", token, {"preset": "vitamin_d3"})

# A 16:8 fast that began after last night's dinner.
call("POST", "/fasting/start", token, {"targetHours": 16, "startedAt": (midnight - dt.timedelta(hours=3)).isoformat().replace("+00:00", "Z")})

# Screen time, the body, and where it hurts.
call("PUT", "/screen-time", token, {"minutes": 248, "source": "shortcut"})
call("PUT", "/settings/target-weight", token, {"targetWeightKg": 80})
call("POST", "/health/blood-pressure", token, {"systolic": 122, "diastolic": 79})
for region, severity in [("lower_back", 2), ("quads", 2), ("glutes", 1)]:
    call("PUT", f"/soreness/{region}", token, {"severity": severity})
call("POST", "/trackers", token, {"name": "Dentist", "lastDoneOn": (today - dt.timedelta(days=120)).isoformat(), "intervalMonths": 6})

# A check-in, so the streak is not zero.
call("PUT", "/check-ins/today", token, {"mood": 4, "energy": 3, "wins": "Slept well, a bit stiff from leg day"})

print(f"email={email}")
print(f"password={password}")
