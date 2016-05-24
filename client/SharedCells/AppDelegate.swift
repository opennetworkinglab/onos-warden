//
//  AppDelegate.swift
//  SharedCells
//
//  Created by Thomas Vachuska on 5/13/16.
//  Copyright © 2016 Thomas Vachuska. All rights reserved.
//

import Cocoa

@NSApplicationMain
class AppDelegate: NSObject, NSApplicationDelegate, NSUserNotificationCenterDelegate {

    @IBOutlet weak var window: NSWindow!

    let wardenUrl = "http://10.254.1.19:4321/"
    let pollSeconds = 60.0
    let showSeconds = 7.0
    let warnMinutes = 5
    
    let username = NSUserName()
    let center = NSUserNotificationCenter.defaultUserNotificationCenter()
    let statusItem = NSStatusBar.systemStatusBar().statusItemWithLength(-2)
    let popover = NSPopover()

    var timer: NSTimer?
    var closeTimer: NSTimer?
    var notificationTimer: NSTimer?
    var eventMonitor: EventMonitor?
    var notification: NSUserNotification?
    
    var hadReservation = false
    var cellsTableController: SharedCellsViewController?

    // Start-up hook
    func applicationDidFinishLaunching(aNotification: NSNotification) {
        if let button = statusItem.button {
            button.image = NSImage(named: "Image")
            button.action = #selector(AppDelegate.togglePopover(_:))
        }
        
        popover.contentViewController = SharedCellsViewController(nibName: "SharedCellsViewController", bundle: nil)
        
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "View Cells", action: #selector(viewCells(_:)), keyEquivalent: "s"))
        menu.addItem(NSMenuItem.separatorItem())
        menu.addItem(NSMenuItem(title: "Borrow Standard Cell", action: #selector(borrow31Cell), keyEquivalent: "b"))
        
        let subMenuItem = NSMenuItem(title: "Borrow Custom Cell", action: nil, keyEquivalent: "")
        menu.addItem(subMenuItem)
        
        let subMenu = NSMenu()
        subMenu.addItem(NSMenuItem(title: "Borrow 1+1 Cell", action: #selector(borrow11Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 3+1 Cell", action: #selector(borrow31Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 5+1 Cell", action: #selector(borrow51Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 7+1 Cell", action: #selector(borrow71Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem.separatorItem())
        subMenu.addItem(NSMenuItem(title: "Borrow 1+0 Cell", action: #selector(borrow10Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 3+0 Cell", action: #selector(borrow30Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 5+0 Cell", action: #selector(borrow50Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 7+0 Cell", action: #selector(borrow70Cell), keyEquivalent: ""))
        menu.setSubmenu(subMenu, forItem: subMenuItem)

        menu.addItem(NSMenuItem.separatorItem())
        menu.addItem(NSMenuItem(title: "Return Cell", action: #selector(returnCell(_:)), keyEquivalent: "r"))
        menu.addItem(NSMenuItem.separatorItem())
        menu.addItem(NSMenuItem(title: "Quit", action: #selector(NSApplication.terminate), keyEquivalent: "q"))
        statusItem.menu = menu
        
        center.delegate = self
        cellsTableController = popover.contentViewController as? SharedCellsViewController
        
        timer = NSTimer.scheduledTimerWithTimeInterval(pollSeconds, target: self,
                                                       selector: #selector(checkForExpiration),
                                                       userInfo: nil, repeats: true)
        
        eventMonitor = EventMonitor(mask: [.LeftMouseDownMask, .RightMouseDownMask]) { [unowned self] event in
            if self.popover.shown {
                self.closePopover(event)
            }
        }
        eventMonitor?.start()
    }

    // Tear-down hook
    func applicationWillTerminate(aNotification: NSNotification) {
        // Insert code here to tear down your application
        timer?.invalidate()
        center.removeAllDeliveredNotifications()
    }

    // Obtains data on cell status and displays it in a pop-up window
    func viewCells(sender: AnyObject?) {
        request("\(wardenUrl)/data", method: "GET", stringData: nil, callback: updatePopover)
        showPopover(self)
    }
    
    func borrow11Cell() { borrowCell("1%2B1") }
    func borrow31Cell() { borrowCell("3%2B1") }
    func borrow51Cell() { borrowCell("5%2B1") }
    func borrow71Cell() { borrowCell("6%2B1") }
    func borrow10Cell() { borrowCell("1%2B0") }
    func borrow30Cell() { borrowCell("3%2B0") }
    func borrow50Cell() { borrowCell("5%2B0") }
    func borrow70Cell() { borrowCell("7%2B0") }
    
    // Borrows cell for the user and for 60 minutes into the future
    func borrowCell(cellSpec: String) {
        self.showNotification("Allocating cell", text: "Please wait for confirmation", action: nil, sound: false)
        request("\(wardenUrl)?duration=60&user=\(username)&spec=\(cellSpec)", method: "POST",
                stringData: userKey()! as String, callback: { response in
            self.notification = self.showNotification("Cell is allocated and ready",
                                                     text: "Reservation is valid for 60 minutes", action: nil, sound: false)
            self.scheduleNotificationDismissal()
        })
    }

    // Returns cell currently leased by the user
    func returnCell(sender: AnyObject?) {
        hadReservation = false
        self.showNotification("Returning cell", text: "Tearing down the environment", action: nil, sound: false)
        request("\(wardenUrl)?user=\(username)", method: "DELETE", stringData: nil, callback: { response in
            self.notification = self.showNotification("Cell returned", text: "Thank you for cleaning up!", action: nil, sound: false)
            self.scheduleNotificationDismissal()
        })
    }

    func extendLease() {
        request("\(wardenUrl)?duration=60&user=\(username)", method: "POST",
                stringData: userKey()! as String, callback: { response in
            self.notification = self.showNotification("Cell lease extended", text: "Reservation is valid for 60 minutes", action: nil, sound: false)
            self.scheduleNotificationDismissal()
        })
    }
    
    func userKey() -> NSString? {
        let home = NSHomeDirectory()
        let sshKeyFilePath = home.stringByAppendingString("/.ssh/id_rsa.pub") as String
        return try? NSString(contentsOfFile: sshKeyFilePath, encoding: NSUTF8StringEncoding)
    }

    func updatePopover(data: NSString) {
        cellsTableController?.updateCellData(data)
    }

    func showPopover(sender: AnyObject?) {
        if let button = statusItem.button {
            popover.showRelativeToRect(button.bounds, ofView: button, preferredEdge: NSRectEdge.MinY)
            closeTimer = NSTimer.scheduledTimerWithTimeInterval(showSeconds, target: self,
                                                                selector: #selector(closePopover(_:)),
                                                                userInfo: nil, repeats: false)
        }
        eventMonitor?.start()
    }

    func closePopover(sender: AnyObject?) {
        closeTimer?.invalidate()
        popover.performClose(sender)
        eventMonitor?.stop()
    }

    func togglePopover(sender: AnyObject?) {
        if popover.shown {
            closePopover(sender)
        } else {
            showPopover(sender)
        }
    }

    func scheduleNotificationDismissal() {
        closeTimer = NSTimer(timeInterval: showSeconds, target: self, selector: #selector(dismissNotification), userInfo: nil, repeats: false)
        NSRunLoop.mainRunLoop().addTimer(closeTimer!, forMode: NSRunLoopCommonModes)
    }
    
    func dismissNotification() {
        if notification != nil {
            center.removeDeliveredNotification(notification!)
        }
    }

    func showNotification(title: String, text: String, action: String?, sound: Bool) -> NSUserNotification {
        center.removeAllDeliveredNotifications()
        let notification = NSUserNotification()
        notification.title = title
        notification.informativeText = text
        notification.hasActionButton = action != nil
        if notification.hasActionButton {
            notification.actionButtonTitle = action!
        }
        if sound {
            notification.soundName = NSUserNotificationDefaultSoundName
        }

        center.scheduleNotification(notification)
        return notification
    }

    func userNotificationCenter(center: NSUserNotificationCenter, shouldPresentNotification notification: NSUserNotification) -> Bool {
        return true
    }

    func userNotificationCenter(center: NSUserNotificationCenter, didActivateNotification notification: NSUserNotification) {
        if notification.activationType == .ActionButtonClicked {
            extendLease()
        }
    }

    func checkForExpiration() {
        request("\(wardenUrl)/data?user=\(username)", method: "GET", stringData: nil, callback: { (data) in
            let record = data.stringByTrimmingCharactersInSet(NSCharacterSet.newlineCharacterSet())
            if !record.hasPrefix("null") {
                var fields = record.componentsSeparatedByString(",")
                let remaining = fields.count > 3 ? Int(fields[3]) : 0
                if remaining != nil && remaining < self.warnMinutes {
                    self.hadReservation = true
                    self.showNotification("Cell reservation is about to expire",
                                          text: "You have less than \(remaining! + 1) minutes left", action: "Extend", sound: true)
                }
            } else if self.hadReservation {
                self.hadReservation = false
                self.showNotification("Cell reservation expired", text: "The cell has been returned", action: nil, sound: true)
            }
        })
    }

    func request(urlPath: String, method: String, stringData: String?, callback: (NSString) -> Void) {
        let url: NSURL = NSURL(string: urlPath)!
        let request = NSMutableURLRequest(URL: url)
        request.HTTPMethod = method
        request.HTTPBody = stringData?.dataUsingEncoding(NSUTF8StringEncoding)
        let task = NSURLSession.sharedSession().dataTaskWithRequest(request) { data, response, error in
            guard error == nil && data != nil else {
                print("error = \(error)")
                return
            }
            if let httpStatus = response as? NSHTTPURLResponse where httpStatus.statusCode != 200 {
                print("status = \(httpStatus.statusCode)\nresponse = \(response)")
            }
            
            callback(NSString(data: data!, encoding: NSUTF8StringEncoding)!)
        }
        task.resume()
    }
}

