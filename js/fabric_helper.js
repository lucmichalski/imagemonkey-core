function setCanvasBackgroundImageUrl(canvas, url, callback) {
    if (url && url.length > 0) {
        fabric.Image.fromURL(url, function (img) {
            scaleAndPositionImage(canvas, img, callback);
        });
    } else {
        canvas.backgroundImage = 0;
        canvas.setBackgroundImage('', canvas.renderAll.bind(canvas));

    }
}

function colorComponentToHex(c) {
    var hex = c.toString(16);
    return hex.length == 1 ? "0" + hex : hex;
}

function rgbToHex(r, g, b) {
    return "#" + colorComponentToHex(r) + colorComponentToHex(g) + colorComponentToHex(b);
}

function registerColorPickerOnMove(canvas, ctx, callback){
    canvas.on('mouse:move', function(o) {
      var pointer = canvas.getPointer(o.e);
      var x = parseInt(pointer.x);
      var y = parseInt(pointer.y);

      // get the color array for the pixel under the mouse
      var px = ctx.getImageData(x, y, 1, 1).data;
      callback(rgbToHex(px[0], px[1], px[2]))
    });
}

function scaleAndPositionImage(canvas, img, callback) {
    var scaleFactor = calcScaleFactor(img, 600.0);
    canvas.setBackgroundImage(img,
        canvas.renderAll.bind(canvas), {
            scaleX: scaleFactor,
            scaleY: scaleFactor
        }
    );
    canvas.setWidth(img.width * scaleFactor);
    canvas.setHeight(img.height * scaleFactor);
    canvas.calcOffset();

    canvas.renderAll();
    typeof callback === 'function' && callback();
}

function calcScaleFactor(img, maxImageWidth){
    //on mobile, make image full width
    var isMobile = window.matchMedia("only screen and (max-width: 760px)");
    if (isMobile.matches) {
        maxImageWidth = document.body.clientWidth - 70;
    }
    var scaleFactor = maxImageWidth/img.width;
    if(scaleFactor > 1.0)
        scaleFactor = 1.0;

    return scaleFactor;
}

function drawAnnotations(canvas, annotations, scaleFactor){
  for(var i = 0; i < annotations.length; i++){
    var type = annotations[i]["type"];
    if(type === "rect"){
        var top = (annotations[i]["top"] * scaleFactor);
        var left = (annotations[i]["left"] * scaleFactor);
        var height = (annotations[i]["height"] * scaleFactor);
        var width = (annotations[i]["width"] * scaleFactor);
        var angle = annotations[i]["angle"];

        var rect = new fabric.Rect({
            left: left,
            top: top,
            originX: 'left',
            originY: 'top',
            width: width,
            height: height,
            angle: angle,
            stroke: 'red',
            strokeWidth: 5,
            fill: "transparent",
            transparentCorners: false,
            hasBorders: false,
            hasControls: false,
            selectable: false
        });
        canvas.add(rect);      
    }
    else if(type === "ellipse"){
        var top = (annotations[i]["top"] * scaleFactor);
        var left = (annotations[i]["left"] * scaleFactor);
        var rx = (annotations[i]["rx"] * scaleFactor);
        var ry = (annotations[i]["ry"] * scaleFactor);
        var angle = annotations[i]["angle"];

        var ellipsis = new fabric.Ellipse({
            left: left,
            top: top,
            originX: 'left',
            originY: 'top',
            rx: rx,
            ry: ry,
            angle: angle,
            stroke: 'red',
            strokeWidth: 5,
            fill: "transparent",
            transparentCorners: false,
            hasBorders: false,
            hasControls: false,
            selectable: false
        });
        canvas.add(ellipsis);
    }
    else if(type === "polygon"){
        var top = (annotations[i]["top"] * scaleFactor);
        var left = (annotations[i]["left"] * scaleFactor);
        var angle = annotations[i]["angle"];
        var points = annotations[i]["points"];
        var scaledPoints = [];

        for(var j = 0; j < points.length; j++){
            scaledPoints.push({"x": (points[j]["x"] * scaleFactor), "y": (points[j]["y"] * scaleFactor)});
        }

        var polygon = new fabric.Polygon(scaledPoints, {
            left: left,
            top: top,
            originX: 'left',
            originY: 'top',
            angle: angle,
            stroke: 'red',
            strokeWidth: 5,
            fill: "transparent",
            transparentCorners: false,
            hasBorders: false,
            hasControls: false,
            selectable: false
        });
        canvas.add(polygon);
    }
}

canvas.renderAll();
}





var CanvasDrawer = (function () {
    function CanvasDrawer(id, width, height){
        this.canvas = new fabric.Canvas(id);
        this.backgroundImageUrl = null;
        this.callback = null;
        this.img = null;
        this.maxImageWidth = 600;
        this.canvasId = id;
        this.canvasWidth = width;
        this.canvasHeight = height;  
    }

    /*function calculateScaleFactor(canvasId, img) {
        var scaleFactor = canvas.width/img.width;
        return scaleFactor;
    }*/


    function scaleAndPositionImg(canvas, img, canvasWidth, canvasHeight, callback) {
        var scaleFactor = canvasWidth/img.width;
        if(scaleFactor > 1.0)
            scaleFactor = 1.0;


        canvas.setBackgroundImage(img,
            canvas.renderAll.bind(canvas), {
                scaleX: scaleFactor,
                scaleY: scaleFactor
            }
        );
        canvas.setHeight(canvasHeight);
        canvas.setWidth(canvasWidth);

        canvas.calcOffset();

        canvas.renderAll();
        typeof callback === 'function' && callback();
    }
    

    CanvasDrawer.prototype.setCanvasBackgroundImageUrl = function(url, callback) {
        var inst = this;
        this.backgroundImageUrl = url;
        this.callback = callback;

        if (url && url.length > 0) {
        fabric.Image.fromURL(url, function (img) {
            this.img = img;
            scaleAndPositionImg(inst.canvas, img, inst.canvasWidth, inst.canvasHeight, callback);
        });
        } else {
            this.canvas.backgroundImage = 0;
            this.canvas.setBackgroundImage('', this.canvas.renderAll.bind(this.canvas));

        }
    }


    CanvasDrawer.prototype.drawAnnotations = function(annotations){
        drawAnnotations(this.canvas, annotations, this.canvas.backgroundImage.scaleX);
    }

    CanvasDrawer.prototype.maxImageWidth = function(maxImageWidth){
        this.maxImageWidth = maxImageWidth;
    }


    return CanvasDrawer;
}());