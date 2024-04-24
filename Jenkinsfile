@Library(['jaazy-jeff-jaas', 'jaas-workflow']) _

//to use docker uncomment this and use agent_label: node(agent_label)
def agent_label = jaas.getDockerAgentLabel()

node(agent_label) {
    cleanWs()
    checkout scm
    GIT_COMMIT = sh(returnStdout: true, script: 'git rev-parse HEAD').trim()

    def j = jaazy()

    def buildEnv = j.env("Environment")
        .addVariables([
            "DOCKER_BUILDKIT": "1"
        ])
        .addCredentialEnv(usernamePassword(
            credentialsId: 'bbgithub_token',
            usernameVariable: 'GIT_USERNAME',
            passwordVariable: 'GIT_PASSWORD'
        ))

    def infoAgent = j.agent("generic.JaazyFileInfoAgent")

    // WORKFLOW CONTROLLER
    def workflow_controller = j.agent("generic.DockerAgent", "workflow-controller")
                    .setDefaultNamespace("workflow-runtimes")
                    .setDockerRegistryCredential("dsbuild-artifactory-jwt")
                    .inside(buildEnv)
                    .setSanitizeNameClosure({ rawName -> return "workflow-controller" })
                    .addBuildVariables([
                        "GIT_COMMIT": "${GIT_COMMIT}",
                        "GIT_TREE_STATE": "clean",
                        "GIT_TAG": infoAgent.getVersion(),
                        "VERSION": infoAgent.getVersion(),
                    ])
                    .addBuildFlags(["--target workflow-controller"])


    // ARGOEXEC
    def argoexec = j.agent("generic.DockerAgent", "argoexec")
                    .setDefaultNamespace("workflow-runtimes")
                    .setDockerRegistryCredential("dsbuild-artifactory-jwt")
                    .inside(buildEnv)
                    .setSanitizeNameClosure({ rawName -> return "argoexec" })
                    .addBuildVariables([
                        "GIT_COMMIT": "${GIT_COMMIT}",
                        "GIT_TREE_STATE": "clean",
                        "GIT_TAG": infoAgent.getVersion(),
                        "VERSION": infoAgent.getVersion(),
                    ])
                    .addBuildFlags(["--target argoexec"])

    // UI
    def argoui = j.agent("generic.DockerAgent", "argoui")
              .setDefaultNamespace("workflow-runtimes")
              .setDockerRegistryCredential("dsbuild-artifactory-jwt")
              .inside(buildEnv)
              .setSanitizeNameClosure({ rawName -> return "argoui" })
              .addBuildVariables([
                  "GIT_COMMIT": "${GIT_COMMIT}",
                  "GIT_TREE_STATE": "clean",
                  "GIT_TAG": infoAgent.getVersion(),
                  "VERSION": infoAgent.getVersion(),
                  "HTTP_PROXY": "http://devproxy.bloomberg.com:82",
                  "HTTPS_PROXY": "http://devproxy.bloomberg.com:82",
                  "NO_PROXY": ".bloomberg.com",
              ])
              .addBuildFlags(["--secret id=GIT_PASSWORD,env=GIT_PASSWORD", "--target argo-ui"])

    // most curiously, the server is embedded into the argocli, and started via `argo server`
    def argocli = j.agent("generic.DockerAgent", "argocli")
              .setDefaultNamespace("workflow-runtimes")
              .setDockerRegistryCredential("dsbuild-artifactory-jwt")
              .inside(buildEnv)
              .setSanitizeNameClosure({ rawName -> return "argocli" })
              .addBuildVariables([
                  "GIT_COMMIT": "${GIT_COMMIT}",
                  "GIT_TREE_STATE": "clean",
                  "GIT_TAG": infoAgent.getVersion(),
                  "VERSION": infoAgent.getVersion(),
                  "HTTP_PROXY": "http://devproxy.bloomberg.com:82",
                  "HTTPS_PROXY": "http://devproxy.bloomberg.com:82",
                  "NO_PROXY": ".bloomberg.com",
              ])
              .addBuildFlags(["--secret id=GIT_PASSWORD,env=GIT_PASSWORD", "--target argocli"])

    j.workflow("SimpleFlow")
                .infoUsing(infoAgent)
                .buildUsingParallel([workflow_controller, argoexec, argoui, argocli])
                .publishUsing(workflow_controller)
                .publishUsing(argoexec)
                .publishUsing(argoui)
                .publishUsing(argocli)
                .start()


}
