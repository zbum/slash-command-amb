# slash-command-amb

Dooray 메신저에서 `/amb` 슬래시 커맨드를 통해 AMB 배포 정보를 채널에 공유하는 서비스.

## 동작 방식

```
사용자: /amb 입력
         │
         ▼
┌─────────────────────┐
│    Dialog 표시       │
│                     │
│  pubo   [O / X]     │
│  fino   [O / X]     │
│  govo   [O / X]     │
│  govi   [O / X]     │
│  pppng  [O / X]     │
│  업무 URL [________] │
│                     │
│        [공유]        │
└─────────────────────┘
         │
         ▼
채널에 메시지 공유:

  [AMB 공유]
  - Zone: pubo, govo, pppng
  - 업무 URL: https://...
```

## API Endpoints

| Method | Path | 설명 |
|--------|------|------|
| POST | `/command` | 슬래시 커맨드 수신 (Request URL) |
| POST | `/interactive` | Dialog 제출 처리 (Interactive Request URL) |
| GET | `/health` | 헬스체크 |

## 로컬 실행

```bash
# 빌드 & 실행
make run

# 또는 직접 실행
PORT=8080 go run main.go
```

기본 포트는 `8080`이며, `PORT` 환경변수로 변경 가능.

## 빌드

```bash
make build            # 로컬 바이너리 → dist/
make build-all        # 크로스 컴파일 (linux, darwin, windows)
```

## Dooray 앱 등록

1. Dooray 메신저 > 설정 > 슬래시 커맨드 > 앱 추가
2. 커맨드 추가: `/amb`
3. URL 설정:

| 항목 | 값 |
|------|---|
| Request URL | `https://{서버주소}/command` |
| Interactive Request URL | `https://{서버주소}/interactive` |

## 배포

Jenkins 파이프라인으로 K8s에 자동 배포됩니다.

```
Checkout → Test → Build Image → Push → Deploy to K8s
```

```bash
# 수동 배포 시
kubectl apply -f k8s/
```

### K8s 리소스

```
k8s/
├── namespace.yaml      # slash-command 네임스페이스
├── deployment.yaml     # Pod 배포 (probe 포함)
├── service.yaml        # ClusterIP 80 → 8080
└── ingress.yaml        # 외부 접근 설정
```

## 프로젝트 구조

```
.
├── main.go             # 슬래시 커맨드 서버
├── Dockerfile          # 멀티스테이지 빌드
├── Jenkinsfile         # CI/CD 파이프라인
├── Makefile            # 빌드 스크립트
├── go.mod
└── k8s/                # Kubernetes 매니페스트
```
