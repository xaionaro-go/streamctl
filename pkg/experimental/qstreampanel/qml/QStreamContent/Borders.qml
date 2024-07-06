import QtQuick 2.15
import QtQuick.Controls 2.15

Rectangle
{
    SystemPalette {
        id: palette
        colorGroup: SystemPalette.Active
    }

    property int lBorderWidth : 1
    property int rBorderWidth : 1
    property int tBorderWidth : 1
    property int bBorderWidth : 1

    z : -1

    property string borderColor : palette.midlight

    color: "transparent"

    anchors.fill: parent
    Rectangle {
        width:parent.width-20
        height:parent.height-20
        border.color: "#ff0000"
        border.width: 5
    }
}
