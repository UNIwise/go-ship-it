#!groovy
@Library('utils')
import jenkinslib.Utilities
import jenkinslib.PodBuilder
def utils = new Utilities(this, false)
def podbuilder = new PodBuilder(this, false)

properties([
  parameters([
    string(name: 'docker tags', defaultValue: utils.tagsFromBranch(env.BRANCH_NAME).join(","), description: "Tag the docker image with these tags. (separated by commas)")
  ])
])

podbuilder.Golang(){ 
  def tags = params["docker tags"].tokenize(",")
  stage('Checkout'){
    tags.push((checkout(scm)).GIT_COMMIT.substring(0,7))
  }

  container('golang'){
    stage('Test'){
      sh "go test ./..."
    }
  }

  container('dind'){
    stage('Build'){
      withCredentials([usernamePassword(credentialsId: "jenkins-harbor-registry", usernameVariable: 'USERNAME', passwordVariable: 'PASSWORD')]){
        sh "docker login --username='$USERNAME' --password='$PASSWORD' docker.wiseflow.io"
      }
      sh "docker build . -t " + utils.dockertags(tags).join(" -t ")
    }
    stage('Push'){
      utils.dockertags(tags).each {
        tag -> sh "docker push $tag"
      }
    }
  }
}