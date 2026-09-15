p = 'backend/controllers/security.go'
lines = open(p, 'rb').read().decode('utf-8').splitlines()
for i, l in enumerate(lines):
    if 'func RequestVerification' in l:
        print(i+1, l)
        for j in range(i, i+30):
            print(lines[j])
        break