pipeline {
  agent any
  options {
    timestamps()
    disableConcurrentBuilds()
    buildDiscarder(logRotator(numToKeepStr: '20'))
    timeout(time: 30, unit: 'MINUTES')
  }
  environment {
    APP         = 'peelen-ms-be'      // nama image, container, dan project compose
    HEALTH_URL  = 'http://peelen-ms-be:8080/health'
    NETWORK     = 'peelen-network'    // network dari docker-compose.yml
    ENV_CRED_ID = 'peelen-ms-be-env'  // credential Secret file di Jenkins
  }
  stages {
    stage('Test') {
      steps {
        sh 'docker build --target test -t $APP:test .'
      }
    }
    stage('Build') {
      steps {
        sh 'docker build -t $APP:$GIT_COMMIT .'
      }
    }
    stage('Deploy') {
      when { anyOf { branch 'master'; branch 'ci/docker-jenkins' } }
      steps {
        withCredentials([file(credentialsId: env.ENV_CRED_ID, variable: 'ENV_FILE')]) {
          sh '''
            install -m 600 "$ENV_FILE" .env
            IMAGE_TAG=$GIT_COMMIT docker compose up -d --no-build --remove-orphans
          '''
        }
      }
    }
    stage('Health check') {
      when { anyOf { branch 'master'; branch 'ci/docker-jenkins' } }
      steps {
        // Jenkins tidak join ke peelen-network, jadi curl dijalankan dari container sementara di network itu.
        // GIN_MODE=release menolak HTTP polos (426), jadi kirim header proxy TLS.
        // Butuh TRUSTED_PROXY_CIDRS=172.30.10.0/24 di .env.
        sh '''
          for i in $(seq 1 30); do
            docker run --rm --network "$NETWORK" curlimages/curl:8.10.1 \
              -fsS -H 'X-Forwarded-Proto: https' "$HEALTH_URL" && exit 0
            sleep 2
          done
          echo "Health check gagal"; docker logs --tail 100 $APP; exit 1
        '''
        sh 'docker tag $APP:$GIT_COMMIT $APP:latest'
      }
    }
  }
  post {
    always {
      sh 'rm -f .env'
      cleanWs()
    }
    success {
      // simpan 5 image terakhir untuk rollback, hapus sisanya
      sh '''
        docker images $APP --format '{{.Tag}}' | grep -vE '^(latest|test)$' \
          | tail -n +6 | xargs -r -I{} docker rmi $APP:{} || true
        docker image prune -f
      '''
    }
  }
}
