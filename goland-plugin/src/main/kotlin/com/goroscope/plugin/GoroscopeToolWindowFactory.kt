package com.goroscope.plugin

import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindow
import com.intellij.openapi.wm.ToolWindowFactory
import com.intellij.ui.content.ContentFactory

/**
 * GoroscopeToolWindowFactory creates the "Goroscope" side panel registered in
 * plugin.xml.  It adds two tabs: **Session** (status tree) and **Timeline**
 * (JCEF browser showing the full Goroscope React UI).
 */
class GoroscopeToolWindowFactory : ToolWindowFactory {

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val contentFactory = ContentFactory.getInstance()

        // Session tab – live-polling status panel.
        val sessionPanel = GoroscopeSessionPanel(project, toolWindow)
        val sessionContent = contentFactory.createContent(sessionPanel.component, "Session", false)
        toolWindow.contentManager.addContent(sessionContent)

        // Timeline tab – embedded JCEF browser.
        val timelinePanel = GoroscopeTimelinePanel(project)
        val timelineContent = contentFactory.createContent(timelinePanel.component, "Timeline", false)
        toolWindow.contentManager.addContent(timelineContent)
    }

    override fun shouldBeAvailable(project: Project): Boolean = true
}
