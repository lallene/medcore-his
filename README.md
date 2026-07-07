-- Active: 1782735967289@@dpg-d915ru8js32c7398os8g-a.frankfurt-postgres.render.com@5432@postgres@public
-- Active: 1782735967289@@dpg-d915ru8js32c7398os8g-a.frankfurt-postgres.render.com@5432@medcore_his-postgres.render.com@5432@medcore_his@public
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "admin@medcore.local",
    "password": "admin123"
  }' | jq -r '.data.token')


  curl -s -X PATCH \
  http://localhost:8080/api/consultations/4/status \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "status": "cancelled",
    "cancellationReason": "Patient absent après appel"
  }' | jq



/**
git status
git add .
git commit -m "feat: enrich patient medical summary with consultations and documents"
git push origin main
**/