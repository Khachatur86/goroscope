// GoLand plugin build configuration.
// Requires IntelliJ Platform Gradle Plugin v2 (org.jetbrains.intellij.platform).
// Run with: ./gradlew buildPlugin   (output: build/distributions/goroscope-goland-*.zip)
//           ./gradlew runIde        (launch IDE sandbox for development)
//           ./gradlew publishPlugin (publishes to JetBrains Marketplace when token is set)

plugins {
    id("org.jetbrains.kotlin.jvm") version "1.9.25"
    id("org.jetbrains.intellij.platform") version "2.2.1"
}

group = "com.goroscope"
version = providers.gradleProperty("pluginVersion").getOrElse("0.2.0")

repositories {
    mavenCentral()
    intellijPlatform {
        defaultRepositories()
    }
}

dependencies {
    intellijPlatform {
        // Target GoLand 2024.1+ (IDE build 241.*).  The plugin also installs in
        // any IntelliJ-based IDE (IDEA Ultimate, Rider, CLion, …) because it
        // has no GoLand-specific dependencies.
        goland("2024.1")
        instrumentationTools()
    }
}

intellijPlatform {
    pluginConfiguration {
        name = "Goroscope"
        version = providers.gradleProperty("pluginVersion").getOrElse("0.2.0")
        ideaVersion {
            sinceBuild = "241"
            untilBuild = provider { null } // no upper bound → future-proof
        }
        changeNotes = """
            <h3>0.2.0</h3>
            <ul>
              <li>Timeline webview with JCEF browser (full React UI inside the IDE)</li>
              <li>Run Current Package, Attach to Session, Stop Session, Open Timeline actions</li>
              <li>Session status tool window with live polling</li>
              <li>Click-to-open-source-line from stack frames in the timeline</li>
              <li>Settings page: HTTP address + binary path</li>
            </ul>
        """.trimIndent()
    }

    publishing {
        // Set JETBRAINS_MARKETPLACE_TOKEN env variable in CI (see docs/distribution.md).
        token = providers.environmentVariable("JETBRAINS_MARKETPLACE_TOKEN")
    }

    signing {
        // Required for paid plugins; free plugins can skip signing.
        // Set JETBRAINS_PLUGIN_CERTIFICATE_CHAIN / PRIVATE_KEY / PRIVATE_KEY_PASSWORD
        // env variables for automated signing.
        certificateChain = providers.environmentVariable("JETBRAINS_PLUGIN_CERTIFICATE_CHAIN")
        privateKey = providers.environmentVariable("JETBRAINS_PLUGIN_PRIVATE_KEY")
        password = providers.environmentVariable("JETBRAINS_PLUGIN_PRIVATE_KEY_PASSWORD")
    }
}

kotlin {
    jvmToolchain(17)
}
