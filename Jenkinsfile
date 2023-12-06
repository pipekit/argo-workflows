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

    def infoAgent = j.agent("generic.JaazyFileInfoAgent")

    // WORKFLOW CONTROLLER
    def workflow_controller = j.agent("generic.DockerAgent", "workflow-controller")
                    .setDefaultNamespace("workflow-runtimes")
                    .setDockerRegistryCredential("dsbuild-artifactory-jwt")
                    .inside(buildEnv)
                    .setSanitizeNameClosure({ rawName -> return "workflow-controller" })
                    .addBuildVariables([
                        "GIT_COMMIT": "${GIT_COMMIT}",
                        "GIT_TREE_STATE": "JaaS-clean",
                        "GIT_TAG": "untagged",
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
                        "GIT_TREE_STATE": "JaaS-clean",
                        "GIT_TAG": "untagged",
                        "VERSION": infoAgent.getVersion(),
                    ])
                    .addBuildFlags(["--target argoexec"])

    j.workflow("SimpleFlow")
                .infoUsing(infoAgent)
                .buildUsingParallel([workflow_controller, argoexec])
                .publishUsing(workflow_controller)
                .publishUsing(argoexec)
                .start()

    
}
