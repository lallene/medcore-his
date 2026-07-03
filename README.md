TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "admin@medcore.local",
    "password": "TON_PASSWORD"
  }' | jq -r '.token') && echo "$TOKEN"