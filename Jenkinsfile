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

    def dockerAgent = j.agent("generic.DockerAgent")
                    .setDefaultNamespace("workflow-runtimes")
                    .setDockerRegistryCredential("dsbuild-artifactory-jwt")
                    .inside(buildEnv)
                    .addBuildVariables([
                        "GIT_COMMIT": "${GIT_COMMIT}",
                        "GIT_TREE_STATE": "JaaS-clean",
                        "GIT_TAG": "untagged",
                        "VERSION": infoAgent.getVersion(),
                    ])
                    .addBuildFlags(["--target workflow-controller"])


    def workflow = j.workflow("SimpleFlow") // Probably should give this a better variable name
                .infoUsing(infoAgent)
                .buildUsing(dockerAgent)
                .publishUsing(dockerAgent)
                .start()  // Needed to actually start the workflow
}
